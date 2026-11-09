"""Measure explicitly labeled status latency and peak RSS on macOS in a temporary project."""

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

def summarize(rows):
    """Use nearest-rank p95; resident memory is a maximum, not an allocation sum."""
    if not rows:
        raise ValueError("at least one measured sample is required")
    return {
        "median_seconds": statistics.median(row["seconds"] for row in rows),
        "p95_seconds": sorted(row["seconds"] for row in rows)[
            math.ceil(.95 * len(rows)) - 1
        ],
        "max_rss_bytes": max(row["max_rss_bytes"] for row in rows),
    }


def parse_args(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("--samples", type=int)
    parser.add_argument("--checks", type=int, default=1)
    parser.add_argument("--files", type=int, default=10000)
    parser.add_argument("--bytes", type=int, default=100 * 1024 * 1024)
    parser.add_argument("--cache-mode", choices=("warm-cache", "first-run", "cold-cache"),
                        default="warm-cache")
    args = parser.parse_args(argv)
    if args.cache_mode == "cold-cache":
        parser.error("cold-cache requires a verified OS cache-reset backend; none is implemented")
    if args.samples is None:
        args.samples = 1 if args.cache_mode == "first-run" else 20
    if min(args.samples, args.checks, args.files, args.bytes) < 1:
        parser.error("--samples, --checks, --files and --bytes must be positive")
    if args.cache_mode == "first-run" and args.samples != 1:
        parser.error("first-run measures exactly one first status; use separate invocations")
    return args


def main():
    args = parse_args()
    if platform.system() != "Darwin":
        raise SystemExit("RSS measurement currently requires macOS /usr/bin/time -l")
    binary = args.binary.resolve(strict=True)
    # Hash before fixture preparation; metadata collection is outside timed status runs.
    binary_sha256 = hashlib.sha256(binary.read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix="naru-status-workload-") as temporary:
        root = Path(temporary)
        (root / ".naru").mkdir()
        (root / "src").mkdir()
        names = [f"check{i}" for i in range(args.checks)]
        (root / ".naru/config.yaml").write_text(
            "version: 1\ndefault_mode: code\nmodes: {code: [" + ", ".join(names) + "]}\nchecks:\n"
            + "".join(f"  {name}: {{run: 'true', inputs: [src/**]}}\n" for name in names)
        )
        count, total = args.files, args.bytes
        for i in range(count):
            size = total // count + (i < total % count)
            (root / "src" / f"{i:05d}.txt").write_bytes(b"a" * size)
        subprocess.run(
            [str(binary), "check"], cwd=root, check=True, capture_output=True, timeout=120
        )
        rows = []
        warmups = 1 if args.cache_mode == "warm-cache" else 0
        for i in range(args.samples + warmups):
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
                first_status_seconds = elapsed
            if i >= warmups:  # First-run includes the first status; warm-cache excludes it.
                rows.append({"seconds": elapsed, "max_rss_bytes": rss})
        result = {
            "files": count,
            "bytes": total,
            "samples": len(rows),
            "warmups": warmups,
            "cache_mode": args.cache_mode,
            "cache_evidence": "receipt preparation reads inputs; no OS cache reset",
            "filesystem": "plain-directory, " + args.cache_mode,
            "checks": args.checks,
            "first_status_seconds": first_status_seconds,
            "platform": platform.platform(),
            "machine": platform.machine(),
            "go": subprocess.check_output(["go", "version"], text=True).strip(),
            "binary_sha256": binary_sha256,
            **summarize(rows),
            "samples_data": rows,
        }
        print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
