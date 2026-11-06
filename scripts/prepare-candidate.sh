#!/bin/sh
# Build source-bound, unpublished candidates outside the checkout.
set -eu
GIT_NO_REPLACE_OBJECTS=1
export GIT_NO_REPLACE_OBJECTS
if [ "$#" -ne 1 ]; then
  echo 'usage: sh scripts/prepare-candidate.sh NEW_ABSOLUTE_OUTPUT_DIRECTORY' >&2
  exit 64
fi
case "$1" in /*) ;; *) echo 'output directory must be absolute' >&2; exit 64 ;; esac
root=$(git rev-parse --show-toplevel)
cd "$root"
python3 - "$root" "$1" <<'PYCODE'
import sys
from pathlib import Path
root, destination = (Path(p).resolve() for p in sys.argv[1:])
if destination == root or root in destination.parents:
    raise SystemExit('output must be outside the source checkout')
PYCODE
if [ -n "$(git status --porcelain)" ]; then
  echo 'commit the candidate source before preparing artifacts' >&2
  exit 1
fi
# mkdir refuses an existing destination instead of replacing previous artifacts.
mkdir "$1"
out=$(cd "$1" && pwd -P)
revision=$(git rev-parse HEAD)
version="candidate-$(git rev-parse --short=12 "$revision")"
source_dir=$(mktemp -d "${TMPDIR:-/tmp}/naru-candidate-source.XXXXXX")
trap 'rm -rf "$source_dir"' 0
# Read exact tree/blob objects; archive attributes must not omit/substitute source.
python3 - "$source_dir" "$revision" <<'PYCODE'
import io
import os
import subprocess
import sys
from pathlib import Path, PurePosixPath
root = Path(sys.argv[1]) / 'source'
root.mkdir()
listing = subprocess.check_output(['git', 'ls-tree', '-rz', '--full-tree', sys.argv[2]])
entries = []
for record in listing.split(b'\0'):
    if not record:
        continue
    metadata, name = record.split(b'\t', 1)
    mode, kind, oid = metadata.split()
    path = PurePosixPath(os.fsdecode(name))
    if (mode not in (b'100644', b'100755') or kind != b'blob'
            or path.is_absolute() or '..' in path.parts):
        raise SystemExit('candidate source requires safe regular tracked files')
    entries.append((mode, oid, path))
objects = subprocess.check_output(['git', 'cat-file', '--batch'],
    input=b''.join(oid+b'\n' for _, oid, _ in entries))
stream = io.BytesIO(objects)
for mode, oid, path in entries:
    actual_oid, kind, size = stream.readline().strip().split()
    if actual_oid != oid or kind != b'blob':
        raise SystemExit('unexpected Git object response')
    size = int(size)
    data = stream.read(size)
    if len(data) != size or stream.read(1) != b'\n':
        raise SystemExit('incomplete Git object response')
    target = root.joinpath(*path.parts)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(data)
    target.chmod(0o755 if mode == b'100755' else 0o644)
if stream.read():
    raise SystemExit('unexpected trailing Git objects')
PYCODE
cd "$source_dir/source"
# Disable source overlays, external workspaces, saved Go settings and auto-downloads.
GOENV=off GOWORK=off GOFLAGS= GOTOOLCHAIN=local
export GOENV GOWORK GOFLAGS GOTOOLCHAIN
printf '%s\n' "$revision" > "$out/REVISION"
go version > "$out/TOOLCHAIN"
printf '%s\n' 'GOENV=off GOWORK=off GOFLAGS= GOTOOLCHAIN=local CGO_ENABLED=0' > "$out/BUILD_SETTINGS"
for platform in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${platform%/*}
  arch=${platform#*/}
  name="naru-$os-$arch"
  if [ "$os" = windows ]; then name="$name.exe"; fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -mod=readonly -buildvcs=false -trimpath \
    -ldflags "-X github.com/Chaebin-Park/naru/internal/cli.Version=$version" \
    -o "$out/$name" ./cmd/naru
done
cp LICENSE "$out/LICENSE"
python3 - "$out" <<'PY'
import hashlib
import sys
from pathlib import Path
root = Path(sys.argv[1])
files = sorted(p for p in root.iterdir() if p.is_file())
(root / 'SHA256SUMS').write_text(''.join(
    hashlib.sha256(p.read_bytes()).hexdigest() + '  ' + p.name + '\n'
    for p in files
), encoding='ascii')
PY
printf 'Unpublished candidate: %s\nSource: %s\n' "$out" "$revision"
