# Native Windows path verification

On 2026-11-09, [GitHub Actions run 42572092303](https://github.com/Chaebin-Park/naru/actions/runs/42572092303)
passed native vet and `TestCLIPathLifecycle` at source
`8f2aa7ef91de65895bd62d7f82a67608aa6f57a3`. This is bounded runtime evidence,
not full Windows platform acceptance.

## Recorded environment

- Runner: `windows-2025`, Windows `10.0.26100.33674`, native amd64.
- Go: `go1.23.12 windows/amd64`, `GOTOOLCHAIN=local`.
- Git: `2.56.0.windows.4`.
- Bash: `5.3.15(2)-release`, `x86_64-pc-cygwin`; POSIX `sh` at `/usr/bin/sh`.

The [workflow](../.github/workflows/windows-verification.yml) records actual
versions on every run; the runner image and bundled Git/Bash can change.
Checks continue to require a POSIX shell and use `sh -c`.

## Acceptance matrix

The fixture builds the source CLI, copies the executable into a test-owned
directory and invokes it through real processes. Both executable and project
paths contain the indicated text. Status results must be valid JSON.

| Case | `space path` | `한글 경로` |
| --- | --- | --- |
| `version`, `init` | Exit 0 | Exit 0 |
| Missing receipt | Exit 1, missing, unsatisfied | Exit 1, missing, unsatisfied |
| Fresh `check` and status | Exit 0, pass, satisfied | Exit 0, pass, satisfied |
| Unrelated file edit | Remains pass | Remains pass |
| Declared Korean input edit | Exit 1, stale, unsatisfied | Exit 1, stale, unsatisfied |
| Recheck after that edit | Exit 0, pass, satisfied | Exit 0, pass, satisfied |
| Failing check command | Check/status exit 1, fail, unsatisfied | Check/status exit 1, fail, unsatisfied |
| Restore and recheck | Exit 0, pass, satisfied | Exit 0, pass, satisfied |

Every pass also asserts exactly two declared input files, including a filename
with Korean text and a space. Executable/config/evidence and unrelated files
are excluded from that fixture's inputs. No test case is skipped on Windows.

## Remaining boundaries

This does not verify the full Windows test suite, every pack, a published archive
installer, global PATH installation, cancellation, concurrency, descendant
cleanup or an actual Windows Git pre-push/ref change. Cancellation/recovery and
push-hook acceptance are the next separate work unit. Native vet and the targeted
runtime test do not substitute for those tests. No new cleanup guarantee is made.

Reproduce with Go, Git and POSIX `sh` on PATH:

```sh
go vet ./...
go test -count=1 -v ./internal/cli -run '^TestCLIPathLifecycle$'
```

## Cancellation and push acceptance procedure

`TestWindowsCancellationRecovery` is a Windows-only integration fixture for
runner context cancellation and timeout. It starts test-owned child/grandchild
processes, opens native process handles before cancellation, then observes those
same handles. Surviving descendants are logged as a platform limit. The test
requires unsatisfied status, rejected concurrent execution and blocked restart,
then releases/cleans the fixtures, preserves the affected current record outside
the current-state directory and requires a fresh passing execution. It never
treats an available lock as proof of process termination.

The workflow also runs `TestPrePushLocalRemote` with the explicit
`NARU_TEST_LOCAL_PUSH=1` opt-in. It installs the actual hook and compares real
push outcomes with ref existence in a temporary local bare repository; no
network remote is used. These additions require an actual successful Windows
run before they advance the verified support boundary above.
