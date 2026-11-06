# Current behavior baseline

Updated for NARU-006 on 2026-11-04. This describes the NARU-006 implementation, not a published release. The original NARU-004 baseline was recorded against `a2cc4c0` plus NARU-001. The historical verification result at the end remains unchanged. Current schema, storage order, limits, and recovery are detailed in [execution records](execution-records.md).

Terms: [glossary](glossary.md). Live agent evidence: [verification](verification.md).

## Policy addition (NARU-012)

The [policy contract](policy.md) adds strict rule indexing, executable fixtures, complete findings, explicit bootstrap and reduction-only approval. Policy authority is separate from contract baselines and ordinary check receipts. Missing coverage, stale candidates and changed definitions block approval; this task does not integrate policy commands into Stop/pre-push automatically. The older command table below remains historical for non-policy commands.

## Relay addition (NARU-014)

[Human relay](relay.md) uses schema 3 `human-relay-v1` evidence in the current check slot. Existing schema 2 local runs and legacy compatibility keep their meanings. Requests supersede current evidence with pending authority; accepted artifact-backed results remain subject to current input/definition, expiry and artifact hash checks. Provenance stays human-relayed; CI runs its own actual checks.

## Commands and exit codes

Flags precede positional arguments. Subcommand flag parsing uses Go's `flag` package. Invalid flags return 64; top-level `help`, `-h`, and `--help` return 0. Subcommand help currently returns 64.

| Command | Options and behavior | Exit codes |
| --- | --- | --- |
| `init` | `--force`; detect stack, create config; preserve existing config unless forced; unknown stack gets a failing placeholder | 0 created, 64 existing/usage, 70 write error |
| `check` | `--mode`, `--verbose`, `--evidence`; optional check names; sequential execution; named checks bypass mode selection; validate all names before running | 0 all pass, 1 failed/timeout/error/not-run, duplicate, input mismatch, recovery required, 64 config/name/evidence/empty inputs, 70 internal error, 130 interrupted |
| `status` | `--mode`, `--json`; evaluate without running checks | 0 satisfied, 1 unsatisfied, 64 config/usage error, 70 IO/output/internal error; corrupt evidence is invalid with exit 1 |
| `mode` | Show selection; `name` stores it; `--clear` removes stored choice | 0 success, 64 invalid choice/usage/config, 70 storage error |
| `gate stop` | Read hook JSON from stdin; check selected mode | 0 allow (including unverified escape), 2 block, 64 invalid gate invocation |
| `gate pre-tool` | Guard recognized edits to `.naru/` and mode changes; shell detection is heuristic | 0 allow, 2 block, 64 invalid gate invocation |
| `gate pre-push` | Require default-mode receipts and absence of unverified state; no execution of checks | 0 allow, 1 block, 64 invalid gate invocation |
| `hooks install` | `--force`; preserve existing hooks by default; refuse custom `core.hooksPath` and inherited Git directory overrides | 0 installed, 1 refused/failed, 64 usage/config, 70 project lookup internal error |
| `version`, `--version` | Build override, module version, then `dev` | 0 |

A check command's exit code is stored in the receipt. It is not necessarily the CLI exit code: a command exiting 3 produces a failed receipt with `exit_code: 3` and CLI exit 1.

## Mode and source selection

`check` (without explicit check names) and `status`: `--mode` > `NARU_MODE` > `.naru/state/mode` > `default_mode`. Stop uses the same order without a flag. Pre-push always uses `default_mode`, ignoring stored mode, `NARU_MODE`, `NARU_GATE`, and Stop repeat limits.

Evidence label: `--evidence` > `NARU_EVIDENCE` > `ci` when `CI` is nonempty > `local`. Allowed values are `local`, `agent-run`, `ci`. These labels do not authenticate the caller or prove that CI ran.

## Inputs and evaluation

The input digest covers selected root-relative paths, file contents, executable bits, symlink target strings (not dereferenced target contents), and a definition fingerprint containing `run`, `workdir`, `inputs`, and `exclude`. It does **not** include timeout, toolchain versions, environment variables, or Git HEAD. Legacy receipts may retain HEAD as diagnostic metadata; schema 2 does not use it. Files outside the declared inputs do not invalidate a receipt.

Git listing includes tracked and non-ignored untracked files and expands nested repositories/submodules. Plain directories and roots ignored by an enclosing repository use a filesystem walk. `.naru/**` and Git metadata are always excluded. Other Git listing failures are errors, not silent changes to listing semantics.

Execution state is checked under a per-check lock. Active executions are `running`; leftover running records with no owner are `interrupted`. Empty input sets, absent evidence, invalid/unsupported records, execution-time input changes/read errors, current stale inputs, and failed/cancelled results remain unsatisfied. Only a terminal pass with matched before/after/current evidence can pass. Status JSON schema 2 separates check satisfaction from NOT VERIFIED presence.

## Execution and compatibility

New executions persist running state before shell launch and terminal state after input re-evaluation and checked log completion. Prior state is archived immutably; schema 1 originals are read-only. Strict decoders reject missing/null/duplicate/unknown fields and inconsistent values. A valid legacy pass remains readable but requires a new run; it cannot supply missing after evidence. A corrupt new record never falls back to legacy success.

Cancellation records a terminal result and cannot revive old success. Crashed owners and unaccounted-for children require manual recovery; locks are not expired by PID or TTL. Background processes keeping output open past the wait bound preserve the command result but block verification/restart. [Execution records](execution-records.md) defines platform, process-group, power-loss, and A→B→A limits.

## Stop and push boundaries

Stop searches `CLAUDE_PROJECT_DIR`, hook payload `cwd`, then the process directory. Outside a naru project it allows. Bad config, failed evaluation, or unsatisfied checks block initially. A repeated identical block in the same active Stop chain gives up after the configured repeat allowance (default 3), reports NOT VERIFIED, and saves unverified state. Zero disables the repeat escape. Progress or a new chain resets counting. `NARU_GATE=off` allows with a warning before evaluation; it does not establish success.

Gate-state persistence/removal failures block Stop. A satisfied Stop clears breaker/unverified state and warns when the selected mode is narrower than default. Pre-push requires default-mode satisfaction and blocks if unverified state exists or cannot be inspected, even if malformed or a broken symlink. Pre-push does not clear that state. It examines the working tree, not every ref or commit being pushed. It is not a substitute for clean-checkout CI.

## Remaining limits

NARU-005 addresses strict record validation, same-check locking, cancellation history, before/after hashing, and JSON unverified visibility. It does not provide automatic orphan recovery, authenticated provenance, a filesystem snapshot, minimum-Go runtime evidence, Windows/Linux runtime evidence, or power-loss durability. Mode-read fallback semantics, relay, baseline, and later MVP features remain separate work.

## Regression evidence map

Test names below are executable evidence, not claims that all edge cases are covered.

| Contract | Tests |
| --- | --- |
| Missing/fail/pass/stale; subdirectory; Git/plain directory; status JSON | `TestStopGateFlow` |
| Mode choice, narrowed-mode warning, removed mode | `TestModeSelectsRequiredChecks`, `TestEmptyModeRejectedAndNarrowedModeReported`, `TestGateExplainsRemovedStoredMode` |
| Complete mode precedence (new) | `TestModePrecedenceAtCLI` |
| Evidence precedence and invalid source rejection (new) | `TestEvidencePrecedenceAtCLI` |
| Corrupt JSON blocks status/Stop/pre-push; explicit preserved-state recovery precedes rerun | `TestCorruptReceiptBlocksEvaluation` |
| Schema 1 field preservation and non-matching evidence rejection (new) | `TestReceiptV1Fixture` |
| Definition and input changes | `TestCheckDefinitionChangeIsStale`, `TestComputeTracksContentAndSalt`, `TestComputeTracksSymlinkTargets`, `TestExecutableBitIsAnInput` |
| Empty inputs; nested repositories; ignored roots | `TestEmptyInputsNeverPass`, `TestNestedRepositoriesAreHashed`, `TestRootIgnoredByEnclosingRepository` |
| Invalid config/usage; init behavior | `TestUsageErrors`, `TestGateOutsideProjectAllowsAndBrokenConfigBlocks`, config parse tests, init CLI tests |
| Timeout, cancellation, command exit mapping | `TestCheckTimeoutKillsProcess`, `TestTimeoutKillsProcessGroup`, `TestCancelKillsCheckAndRecordsCancellation`, `TestExitCodeMapping` |
| Stop escapes and progress reset | `TestGateProjectDirAndEscapeHatch`, `TestGateGivesUpAfterIdenticalBlocks`, `TestGateRepeatCounterResetsOnProgress`, `TestGateNeverGivesUpWhenConfigured` |
| State warning and edit guard | `TestGiveUpIsVisibleToHumanAndInStatus`, `TestPreToolGuardsNaruDirectory` |
| Pre-push states/overrides; hook subprocess and installation | `TestPrePushStates`, `TestPrePushIgnoresStopOverrides`, `TestPrePushBrokenUnverifiedSymlink`, `TestHooksInstallAndExecute`, hooks package tests |

New execution tests cover every required field, strict decoding, history collisions, state write/sync/close/directory-sync/rename failures, legacy preservation, duplicate OS processes, root aliases, parent kill with a surviving shell, cancellation/history, and input changes/read errors. `TestCLIExecutionLifecycle` verifies a fresh CLI's active/cancelled/recovered status, exit 130, duplicate exit 1, and Stop/pre-push consistency. At that historical boundary, `TestPrePushLocalRemote` had not run. It subsequently passed after explicit approval on 2026-11-06; see [MVP acceptance](mvp-acceptance.md#approved-actual-local-push-execution). Cross-platform vet is not runtime evidence. No physical power-loss or independent review result is claimed.

## NARU-004 verification result

On 2026-11-04, Go 1.27.3 on macOS arm64 passed the four added test functions (including nine precedence subcases), `gofmt -l .` (empty), `go vet ./...`, `go test -race -count=1 ./...`, Windows/Linux cross-platform vet, and `go build`. The built binary passed `naru check`: fmt, vet, test. The push integration opt-in was unset. Cross-platform vet is static analysis, not runtime verification. No new coverage percentage was measured.

## NARU-006 environment requirements

Optional `requires.tools` (PATH executable names) and `requires.os` (`darwin`, `linux`, `windows`) are checked before input hashing or command execution. Every check implicitly requires `sh`. Unavailable checks persist `not-run` with a reason and no command exit code. They cannot satisfy the gates; restoring the environment requires an explicit rerun. Existing Stop overrides remain NOT VERIFIED. Declared requirements enter the definition fingerprint; omitted requirements preserve its prior form. Tool versions, service health, installation, and remote execution are outside this task. See [execution records](execution-records.md#unavailable-prerequisites) for storage and compatibility details.

## NARU-007 surface foundation

An internal, in-memory schema 1 surface protocol now validates extractor output and produces canonical bytes and SHA-256 content digests. It preserves semantic values and separates input freshness from contract content. This adds no commands or configuration fields and does not alter existing gates. The later NARU-008–010 sections describe the now-implemented loading, execution and approval layers. See [surface contract](surface-contract.md).

## NARU-008 local packs

Config can select local packs and declare surfaces. Pack checks run through the existing check lifecycle; their fingerprints include the selected pack files. Invalid manifests and collisions block loading. Init remains explicit; surface execution is described in NARU-009 below. See [packs](packs.md).

## NARU-009 extraction and diff

Baseline extract/diff commands now create guarded candidates and compare them against read-only snapshots. Failures cannot reuse earlier ready candidates; current inputs and snapshot integrity are rechecked. No automatic baseline replacement or surface gate integration is added. See [baselines](baselines.md).

## NARU-010 approval

`baseline approve` requires terminal stdin/stderr, displays the diff, collects reviewer/reason and full digest confirmation (including an explicit empty marker), then rechecks candidate/input/baseline identity under the surface lock. Snapshot and approval metadata publish atomically in one record. Bare legacy snapshots remain readable but cannot produce unchanged without approval. Failed publication preserves prior bytes; cancellation before publication writes no approval. This is a review workflow, not human authentication or a mode-policy integration. See [baseline approval](baselines.md#interactive-approval).

## NARU-011 Spring Boot example

The repository now includes a limited Spring MVC public sample and local pack with four surfaces. The adapter uses fresh locked Maven exports for API/routes and bounded resource reads for SQL hashes/configuration keys. It does not alter the core evaluator or automatically connect surfaces to check modes. Fixture tests run through Go; actual Maven/CLI smoke is separate evidence. See [support scope](../packs/spring-boot/README.md).

## NARU-033: unresolved process groups

Checks and extractors inspect their Unix process group after the shell exits. Remaining members or inspection errors require manual recovery even when `exec.ExitError` hides `exec.ErrWaitDelay`. Check results retain cancellation/timeout/failure while input evidence becomes unreadable with `owner_lost`; extractor candidates become `recovery/owner_lost`. Caller cancellation retains exit 130. Inspection is bounded and read-only: it does not repeatedly signal a potentially reused group ID. This is not a process sandbox or automatic orphan cleanup.

## NARU-018.1: report evaluation time

Each status evaluation captures one UTC instant and applies it to every relay check in that report. Internal callers can supply the instant with `status.EvaluateAt` to compare reports at acceptance and expiry boundaries. Files and stored evidence are still read from the current workspace; this is not historical replay or an atomic filesystem snapshot. NARU-018 subsequently adds surface/policy aggregation and Markdown reporting, described below.

## NARU-018: common report integration

Opt-in `mode_surfaces` and `mode_policy` now join checks and relay evidence in the shared status decision. Extended reports use schema 3; check-only reports retain schema 2. CLI text/JSON/Markdown and both gate adapters consume the same requirements while retaining explicit Stop escapes and strict pre-push policy. See the [report contract and adapter matrix](common-report.md). Earlier numbered sections describe the scope of their historical tasks.
