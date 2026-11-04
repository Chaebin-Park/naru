#!/bin/sh
# Explicit opt-in setup: repository-local binaries only, no system installation.
set -eu
cd "$(dirname "$0")/.."
platform=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; *) echo 'Unsupported architecture' >&2; exit 1 ;; esac
case "$platform" in darwin|linux) ;; *) echo 'This setup script supports macOS/Linux only' >&2; exit 1 ;; esac
command -v go >/dev/null
command -v curl >/dev/null
mkdir -p .naru/state/devtools/bin
setup_dir=$(mktemp -d .naru/state/devtools/setup-XXXXXX)
trap 'rm -rf "$setup_dir"' EXIT HUP INT TERM
archive="golangci-lint-2.15.0-$platform-$arch.tar.gz"
base=https://github.com/golangci/golangci-lint/releases/download/v2.15.0
curl -fsSL "$base/$archive" -o "$setup_dir/$archive"
curl -fsSL "$base/golangci-lint-2.15.0-checksums.txt" -o "$setup_dir/checksums"
expected=$(awk -v file="$archive" '$2 == file {print $1}' "$setup_dir/checksums")
if command -v sha256sum >/dev/null; then
 actual=$(sha256sum "$setup_dir/$archive" | awk '{print $1}')
else
 actual=$(shasum -a 256 "$setup_dir/$archive" | awk '{print $1}')
fi
[ -n "$expected" ] && [ "$actual" = "$expected" ] || { echo 'Checksum verification failed' >&2; exit 1; }
tar -xzf "$setup_dir/$archive" -C "$setup_dir" "golangci-lint-2.15.0-$platform-$arch/golangci-lint"
cp "$setup_dir/golangci-lint-2.15.0-$platform-$arch/golangci-lint" .naru/state/devtools/bin/golangci-lint
GOTOOLCHAIN=local GOBIN="$PWD/.naru/state/devtools/bin" go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
.naru/state/devtools/bin/golangci-lint version
printf '%s\n' 'Development tools are ready under .naru/state/devtools/bin.'
