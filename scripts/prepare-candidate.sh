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
version="candidate-$(git rev-parse --short=12 HEAD)"
printf '%s\n' "$revision" > "$out/REVISION"
go version > "$out/TOOLCHAIN"
for platform in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  os=${platform%/*}
  arch=${platform#*/}
  name="naru-$os-$arch"
  if [ "$os" = windows ]; then name="$name.exe"; fi
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
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
