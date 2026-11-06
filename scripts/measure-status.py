"""Measure warm-cache status latency and peak RSS on macOS in a temporary project."""

import argparse
import hashlib
import json
import math
import platform
import statistics
import subprocess
import tempfile
import time
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binary", type=Path)
parser.add_argument("--samples", type=int, default=20)
parser.add_argument("--checks", type=int, default=1)
args = parser.parse_args()
if platform.system() != "Darwin":
    parser.error("RSS measurement currently requires macOS /usr/bin/time -l")
if args.samples < 1 or args.checks < 1:
    parser.error("--samples and --checks must be positive")
binary = args.binary.resolve(strict=True)

with tempfile.TemporaryDirectory(prefix="naru-status-workload-") as temporary:
    root = Path(temporary)
    (root / ".naru").mkdir()
    (root / "src").mkdir()
    names = [f"check{i}" for i in range(args.checks)]
    (root / ".naru/config.yaml").write_text(
        "version: 1\ndefault_mode: code\nmodes: {code: [" + ", ".join(names) + "]}\nchecks:\n"
        + "".join(f"  {name}: {{run: 'true', inputs: [src/**]}}\n" for name in names)
    )
    count, total = 10000, 100 * 1024 * 1024
    for i in range(count):
        size = total // count + (i < total % count)
        (root / "src" / f"{i:05d}.txt").write_bytes(b"a" * size)
    subprocess.run(
        [str(binary), "check"], cwd=root, check=True, capture_output=True, timeout=120
    )
    rows = []
    for i in range(args.samples + 1):
        started = time.perf_counter()
        run = subprocess.run(
            ["/usr/bin/time", "-l", str(binary), "status", "--json"],
            cwd=root, capture_output=True, text=True, check=True, timeout=120,
        )
        elapsed = time.perf_counter() - started
        report = json.loads(run.stdout)
        if (not report["satisfied"] or len(report["checks"]) != args.checks
                or any(check["state"] != "pass" for check in report["checks"])):
            raise RuntimeError("measured status did not verify the fixture")
        rss = int(next(
            line.split()[0] for line in run.stderr.splitlines()
            if "maximum resident set size" in line
        ))
        if not i:
            warmup_seconds = elapsed
        if i:  # One warmup is excluded from the reported samples.
            rows.append({"seconds": elapsed, "max_rss_bytes": rss})
    result = {
        "files": count,
        "bytes": total,
        "samples": len(rows),
        "warmups": 1,
        "filesystem": "plain-directory, warm cache",
        "checks": args.checks,
        "first_status_seconds": warmup_seconds,
        "median_seconds": statistics.median(row["seconds"] for row in rows),
        "platform": platform.platform(),
        "machine": platform.machine(),
        "go": subprocess.check_output(["go", "version"], text=True).strip(),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "p95_seconds": sorted(row["seconds"] for row in rows)[
            math.ceil(.95 * len(rows)) - 1
        ],
        "max_rss_bytes": max(row["max_rss_bytes"] for row in rows),
        "samples_data": rows,
    }
    print(json.dumps(result, indent=2))
