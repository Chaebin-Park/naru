# Android pack verification

NARU-016 verifies the synthetic Java app/library in `examples/android` with the
adapter in `packs/android`. This is a bounded example, not general Android
application or runtime compatibility evidence.

## Environment and provenance

Implementation and smoke work used Go 1.27.3 on macOS, JDK 17.0.19, Gradle
8.11.1, AGP 8.9.2, SDK Platform 35 and Build Tools 35.0.0. Existing tools were
selected per process. No system install, SDK license acceptance, emulator,
device operation or global configuration change was performed. SDK automatic
installation is disabled. Gradle can fetch the pinned plugin dependencies.

The exporter was tested from a temporary copy of the implementation worktree
based on `af85985`; this was not a clean released binary or revision. Local
evidence records source hashes, commands, tool metadata and artifact hashes.
The task ledger records the final implementation revision and isolated full
verification. Runtime sources must match the tested copy before task closure.

The final exporter-only command ran `clean :app:assembleDebug
:app:exportNaruDebug` with a fresh output directory and request ID. It completed
52 tasks in 7 seconds, produced a 9,478-byte debug APK, the provider-backed
merged manifest and a matching SHA-256 export envelope. The app root and local
library both had `module_origin: resolution` and version `1.0.0`. Earlier
implementation runs exposed AGP script-classloader and Gradle task-access
errors; they were corrected before the final successful run.

## Actual CLI smoke

The disposable smoke completed 37 recorded commands/approvals. It used the
already-built NARU relay-era binary; NARU-016 adds no Go production changes.
Every copied runtime source matched the final worktree after restoration. The
only later copy difference was the pack README's activation instructions.

| Scenario | Observed result |
| --- | --- |
| Real clean assembly/export | APK and both fresh intermediate files present; exit 0 |
| Initial manifest/dependency candidates | Extract 0, missing-baseline diff 1, explicit terminal approval 0 |
| Repeat fresh builds | Both diffs unchanged; exit 0 |
| Library INTERNET permission changed to ACCESS_NETWORK_STATE | Expected removal/addition; diff 1; restored diff 0 |
| MainActivity exported true changed to false | Modified component; diff 1; restored diff 0 |
| Library version 1.0.0 changed to 1.0.1 | Resolved project value changed; diff 1; restored diff 0 |
| Failed build with previous valid exports present | Extract 1, failed candidate without snapshot, diff 1 |
| Empty SDK with executables available | Extract 1, failed candidate; no SDK installation |
| sdkmanager absent from PATH | Check 1 with result not-run and missing_tool reason |
| Final restoration | Both diffs unchanged and android.contracts check 0 |

Approved baseline bytes stayed unchanged throughout mutation, failure and
restoration. The smoke compared them directly and kept source hashes, raw
exports, command logs and a final pass summary locally. These private paths and
raw build logs are intentionally not committed. This is actual Android/CLI
evidence, separate from default fake-tool fixtures.

## Automated checks

Go acceptance tests load the real pack, validate normalized fixtures and run the
Python adversarial suite. They distinguish a missing executable (`not-run` for
the build check) from present tools whose build/SDK validation fails. Failed
surface extraction invalidates the candidate and preserves approved baseline
bytes. These ordinary tests do not download Android dependencies.

The Python suite covers permission/exported/dependency changes, deterministic
ordering, strict JSON/XML and graph validation, unsafe paths, bounded copies,
fresh request matching, failed/stale outputs, locks and process cleanup.
Cancellation fixtures wait for an explicit child-start barrier before
interrupting. No Go production behavior or dependencies were added for this pack.

Repository static analysis covers Go lint/security checks and Go vulnerability
analysis. Python syntax and adapter fixtures are additional checks; this is not
a claim that the Go analyzers scan Python or Gradle code for vulnerabilities.

## Limits

Only the debug variant and pinned tool family were exercised. Linux runtime,
Windows runtime, minimum Go 1.23, emulator/device behavior, release signing,
composite builds, arbitrary project layouts and other AGP versions were not
tested. Cross-platform Go vet is distinct from runtime evidence. Actual Git
push tests were not enabled.

Baseline approvals used only a disposable synthetic project. A successful
`android.contracts` build does not enforce approved surface equality. Manifest
projection excludes labels, intent filters, enabled flags and arbitrary
metadata; dependency projection excludes selectors, repositories, constraints
and variant attributes. See the [pack contract](../packs/android/README.md).
