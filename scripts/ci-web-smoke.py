"""Run the public Web pack from a committed source-only archive with CI evidence."""

import argparse
import io
import json
import subprocess
import tarfile
import tempfile
from pathlib import Path, PurePosixPath

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("binary", type=Path)
parser.add_argument("--revision", default="HEAD")
args = parser.parse_args()
binary = args.binary.resolve(strict=True)
revision = subprocess.check_output(
    ["git", "rev-parse", "--verify", "--end-of-options", args.revision + "^{commit}"],
    text=True,
).strip()
archive = subprocess.check_output(
    ["git", "archive", "--format=tar", revision, "packs/web", "examples/web"]
)
print(json.dumps({"revision": revision, "evidence": "ci"}), flush=True)
with tempfile.TemporaryDirectory(prefix="naru-ci-web-") as temporary:
    root = Path(temporary)
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:") as source:
        for entry in source.getmembers():
            path = PurePosixPath(entry.name)
            if path.is_absolute() or ".." in path.parts or not (entry.isdir() or entry.isfile()):
                raise ValueError("archive must contain only safe directories and regular files")
            destination = root.joinpath(*path.parts)
            if entry.isdir():
                destination.mkdir(parents=True, exist_ok=True)
            else:
                destination.parent.mkdir(parents=True, exist_ok=True)
                content = source.extractfile(entry)
                if content is None:
                    raise ValueError("missing archive member")
                with content:
                    destination.write_bytes(content.read())
    (root / ".naru").mkdir()
    (root / ".naru/config.yaml").write_text(
        "version: 1\npacks: [packs/web/pack.yaml]\ndefault_mode: ci\n"
        "modes: {ci: [web.contracts]}\n"
    )
    result = subprocess.run(
        [str(binary), "check", "--mode", "ci", "--evidence", "ci"],
        cwd=root, timeout=120,
    )
    # Status is diagnostic only. Preserve the actual execution exit code.
    subprocess.run([str(binary), "status", "--mode", "ci", "--json"], cwd=root, timeout=30)
    raise SystemExit(result.returncode)
