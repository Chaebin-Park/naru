# Native Linux verification

`linux-verification.yml` runs on main pushes, pull requests, manual dispatch and
scoped `ci/naru-039-*` validation branches. Its Ubuntu 24.04 amd64 matrix tests
Go 1.23.12 and 1.27.3 independently (fail-fast disabled). Actions are SHA-pinned;
permissions are contents:read, checkout credentials are not persisted, timeout is
30 minutes and no logs or receipts are uploaded as artifacts.

Each job prepares pinned golangci-lint 2.15.0 and govulncheck 1.9.0 using Go
1.27.3, then selects and asserts its matrix toolchain with GOTOOLCHAIN=local.
`scripts/ci-go-verify.sh` runs fmt, vet, full race tests, Windows/Linux vet,
application build and executable metadata/version checks. A separate injected
failure test verifies that each of the six Go command failures is propagated
unchanged and stops subsequent commands; CI itself fails on a nonzero helper exit.

Finally Go 1.27.3 runs `naru check`, including development analysis that requires
the newer toolchain. That scan is not evidence of Go 1.23 analysis-tool support.
Actual local-remote push tests remain opt-in and excluded. Results cover only the
recorded source/environment; Windows and Linux arm64 runtime remain unverified.
Tools are installed only in the disposable runner, not on developer machines.

Historical preparation runs: native Go 1.27.3 [42563452238](https://github.com/Chaebin-Park/naru/actions/runs/42563452238)
and minimum Go 1.23.12 [42563978564](https://github.com/Chaebin-Park/naru/actions/runs/42563978564).
