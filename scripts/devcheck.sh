#!/bin/sh
# Build into an ignored, unique directory; concurrent invocations never overwrite a binary.
set -eu
cd "$(dirname "$0")/.."
mkdir -p .naru/state
build_dir=$(mktemp -d .naru/state/devcheck-build-XXXXXX)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
go build -o "$build_dir/devcheck" ./cmd/devcheck
"$build_dir/devcheck" "$@"
