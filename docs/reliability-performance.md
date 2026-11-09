# Reliability, performance and runtime evidence

Measured on 2026-11-06 during NARU-019. Product sources were unchanged from the NARU-018 completion boundary `00c423b`; task base `783f3dd` adds only the NARU-019 plan. The task adds workload fixtures and measurement tooling, not performance optimizations. Results apply to these configurations, not every repository.

## Status workload

The fixture contains exactly 10,000 files totaling 104,857,600 bytes (100 MiB), one required `true` check, a plain directory and warm filesystem cache. `TestLargeWorkspaceStatus` verifies pass, stale after a content edit and pass after exact restoration. The last case explicitly documents that current-state hashing cannot detect an intermediate A→B→A change.

On Apple M4 Pro, macOS 27.2 arm64, Go 1.27.3:

| Measurement | Result |
| --- | --- |
| CLI samples | 20 measured, 1 excluded warmup |
| CLI p95 wall time, nearest rank | 0.317 seconds |
| Maximum sampled process RSS | 15,286,272 bytes (14.58 MiB) |
| Go benchmark, 5 iterations | 306,465,158 ns/op; 342.15 MB/s |
| Go allocations per operation | 347,447,616 bytes; 172,855 allocations |

Allocation totals are not simultaneous resident memory. CLI wall time includes process launch and `/usr/bin/time`; the Go benchmark measures in-process evaluation. These are separate samples. This one-check workload was below 2 seconds; it did not establish the planned three-check target. No cold-cache, multi-check, remote filesystem or large policy/surface performance claim is made. A 256 MiB memory budget remains a planning proposal, not a universal product guarantee.

Measured macOS binary SHA-256: `85f4c670cd6016a0c24be4b1af98d2dde33ffb742a722f1d0a621bda65672e2b`.

Reproduce with an approved installed Go toolchain; the Python CLI measurement currently supports macOS RSS semantics:

```sh
go test ./internal/status -run '^TestLargeWorkspaceStatus$' -count=1
go test ./internal/status -run '^$' -bench '^BenchmarkLargeWorkspaceStatus$' -benchtime=5x -count=1
go build -o /tmp/naru-measure ./cmd/naru
python3 scripts/measure-status.py /tmp/naru-measure --samples 20 > /tmp/naru-status-measurement.json
```

The script creates and removes only its temporary synthetic project, verifies every measured status result and emits individual samples, environment, binary hash, nearest-rank p95 and maximum sampled RSS.

## Linux execution

Used an already-installed Rocky Linux 9 amd64 container image under Docker Desktop on the arm64 host, explicitly selecting `linux/amd64` (emulated userland). No image pull or tool installation occurred. Containers used `--network none`, `--read-only`, a temporary writable `/tmp` and a read-only mount containing only synthetic test binaries. Binaries were cross-built using Go 1.27.3, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`; these Linux runs are not race-instrumented.

- Runner: seven top-level tests, 21 passing results including subtests, zero failed/skipped. Covered duplicate ownership, cancelled execution history, input changes and A→B→A, completion-storage failure, child-cleanup uncertainty and cross-process recovery.
- CLI: four top-level tests, 10 passing results including subtests, zero failed/skipped. Covered common report, surface decisions, policy decisions and relay workflow.
- A separately built Linux CLI binary passed actual command smoke: check/status/pre-push pass for current inputs; status and pre-push both return 1 after mutation. No actual Git push occurred.

The first exploratory CLI selection also included `TestCLIExecutionLifecycle`, which failed because it invokes `go build` and the container has no Go executable. That scenario is **unverified on Linux**, not skipped-as-pass. The final bounded selection excludes it and is reported separately; the prebuilt CLI smoke does not claim equivalent lifecycle coverage. Earlier exploratory mixed-architecture runs are not the basis for the table above.

Reproduce the runner selection with an already available image:

```sh
mkdir -p /tmp/naru-linux-tests
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o /tmp/naru-linux-tests/runner.test ./internal/runner
docker run --rm --pull never --platform linux/amd64 --network none --read-only \
  --tmpfs /tmp:rw,exec,size=256m -v /tmp/naru-linux-tests:/tests:ro rockylinux:9 \
  /tests/runner.test -test.v -test.run '^(TestDuplicateExecutionCannotChangeOwner|TestCancelledExecutionArchivesPass|TestExecutionInputValidity|TestCompletionFailureLeavesRunningAuthority|TestCancellationRecoveryAcrossProcess|TestCheckCancellationRecovery|TestSurfaceCancellationRecovery)$'
```

Linux binary hashes: runner test `6242683bcd693269a0d352ef8991e3f59e9370f95d8309e481d7333782b76018`; CLI test `8e1e21b171c0f7500cb5d881080bb812ac25ea66e4a8344d4517bbb506fb6eec`; CLI `965cbf1e448afad84b1067ca59b4baaf047b0b0d651f425bc7d42479971023f7`.

## Support and remaining limits

| Environment | Evidence | Limit |
| --- | --- | --- |
| macOS arm64 / Go 1.27.3 | Native race suite, vet, self-check, workload measurement | One machine and selected workload |
| Linux amd64, emulated Rocky Linux 9 | Bounded runner/CLI tests and executable smoke above | Not full suite, no race run, build-dependent lifecycle test unverified |
| Windows amd64 / Go 1.23.12 | Native vet, [CLI paths, cancellation/timeout recovery, same-check locking and local pre-push](windows-verification.md) | Descendants survive cancellation/timeout; inspected manual recovery required; POSIX shell required; keyboard Ctrl-C unverified |
| Go 1.23 minimum | Go 1.23.12 build and public Web failure/recovery smoke on GitHub Ubuntu; see [CI evidence](ci-smoke.md) | Full minimum-version test suite remains unverified |

Storage failure injection is covered by the existing runner/receipt tests; no physical power-loss durability claim is made. Per-resource locks do not establish a global filesystem snapshot. Processes that deliberately escape their process group and concurrent same-user state tampering remain outside the supported lifecycle boundary. Actual Git push, remote CI and independent MVP review remain separate tasks.

## Planned three-check measurement (NARU-022)

The acceptance audit found that the earlier 20-sample, one-check measurement was narrower than the original three-check, thirty-sample plan. A supplemental run on the same macOS machine used exactly 10,000 files/100MiB and three required `true` checks, all hashing the same declared inputs. Candidate source `089cbd36dcb10c1e335f0e22ed64cd5698c03a40` contains the same evaluator implementation as the earlier measurement.

```sh
python3 scripts/measure-status.py /path/to/naru --samples 30 --checks 3
```

| Measurement | Result |
| --- | --- |
| Measured samples / excluded warmup | 30 / 1 |
| First status after preparing receipts | 1.035 seconds |
| Median wall time | 1.072 seconds |
| Nearest-rank p95 | 1.109 seconds |
| Maximum sampled RSS | 15,515,648 bytes (14.80 MiB) |

Binary SHA-256: `c18e111e101a2f3f077a2a67ac16c3326d7f0f4cddbf647c0915fc220b42c2de`. Every status was satisfied with all three checks passing. The initial 2-second target was met under the planned warm-cache workload. The first status is not a cold-cache measurement: receipt preparation had already read the inputs. No cache purge was performed and no cold-cache claim is made. Earlier one-check results remain historical evidence rather than being relabeled as three-check measurements.
