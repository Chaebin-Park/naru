#!/bin/sh
# Build source-bound, unpublished candidates outside the checkout.
set -eu
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
# Export the resolved commit once; ignored files and later edits cannot enter builds.
git archive --format=tar -o "$source_dir/source.tar" "$revision"
python3 - "$source_dir" <<'PYCODE'
import sys
import tarfile
from pathlib import Path, PurePosixPath
root = Path(sys.argv[1]) / 'source'
root.mkdir()
with tarfile.open(Path(sys.argv[1]) / 'source.tar') as archive:
    for entry in archive.getmembers():
        path = PurePosixPath(entry.name)
        if path.is_absolute() or '..' in path.parts or not (entry.isdir() or entry.isfile()):
            raise SystemExit('candidate source must contain only safe directories and regular files')
        target = root.joinpath(*path.parts)
        if entry.isdir():
            target.mkdir(parents=True, exist_ok=True)
        else:
            target.parent.mkdir(parents=True, exist_ok=True)
            content = archive.extractfile(entry)
            if content is None:
                raise SystemExit('missing archive member')
            with content:
                target.write_bytes(content.read())
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
