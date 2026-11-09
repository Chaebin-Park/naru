# Native Linux verification

`.github/workflows/linux-verification.yml` prepares an ephemeral Ubuntu 24.04
amd64 runner with Go 1.27.3 and the existing pinned development-tool setup
(golangci-lint 2.15.0 and govulncheck 1.9.0). It asserts Linux x86_64 and Go's
host architecture, records the exact source SHA, and runs format checks, native
vet, the full race suite, Windows/Linux vet, a build and `naru check`.

The workflow has read-only repository permission, no stored checkout credentials,
a 30-minute timeout and no receipt/log artifact uploads. Tool downloads happen
only in the disposable runner. Existing Web smoke is unchanged. Pushes to the
scoped `ci/naru-037-*` branches trigger this preparation workflow; main is not
added as a trigger until the later continuous-verification task.

A successful run establishes results only for its recorded source and environment.
It does not prove the full Go 1.23 suite, Windows runtime or Linux arm64 runtime.
Actual local-remote push tests remain opt-in and excluded from this run. Required
platform tests must execute rather than skip before native coverage is claimed.
