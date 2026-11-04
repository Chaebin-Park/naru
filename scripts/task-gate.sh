#!/bin/sh
# Task transitions create local metadata commits; never push or install tools.
set -eu
cd "$(dirname "$0")/.."
if ! command -v go >/dev/null 2>&1; then
 printf '%s\n' '{"schema":1,"allowed":false,"reasons":["TOOLCHAIN_MISSING"],"next_action":"Put the approved Go toolchain on PATH."}'
 exit 70
fi
mkdir -p .naru/state
build_dir=$(mktemp -d .naru/state/taskgate-build-XXXXXX)
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
if ! GOTOOLCHAIN=local go build -o "$build_dir/taskgate" ./cmd/taskgate; then
 printf '%s\n' '{"schema":1,"allowed":false,"reasons":["BUILD_FAILED"],"next_action":"Repair taskgate before continuing."}'
 exit 70
fi
"$build_dir/taskgate" "$@"
