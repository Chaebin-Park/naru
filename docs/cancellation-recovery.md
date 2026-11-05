# Cancellation recovery verification (NARU-033)

## Problem and decision

A shell can exit while a child in its process group still holds output open. Go's command wait can return an exit error instead of `exec.ErrWaitDelay`, hiding the output wait limit. The old runner then treated a failed or cancelled execution as accounted for and allowed a new execution over the surviving child.

A deterministic regression first reproduced this on the old code: a shell spawned a child and exited nonzero; the result retained matched input evidence with no `owner_lost` reason. This reproduction establishes the classification defect. It does not prove the exact process state of the original intermittent failure.

After command wait, Unix runners inspect the original process group with signal 0 for at most 100ms. Only ESRCH establishes absence. Remaining members, permission errors or other uncertainty require manual recovery. We rejected repeated terminating signals after wait because the numeric group ID can be reused. This change blocks overlap; it does not automatically remove every orphan.

## Acceptance evidence

| Test | Contract |
| --- | --- |
| `TestSurfaceCancellationRecovery` | Cancellation, timeout and nonzero exit with an unaccounted child store recovery; cancellation retains interruption |
| `TestCheckCancellationRecovery` | The execution result remains cancelled/timeout/fail while input evidence becomes unreadable/owner_lost |
| `TestProcessCleanupUncertainty` | Absence, settling, persistent groups and probe errors; no terminating probe signals or invalid group targets |
| `TestCancellationRecoveryAcrossProcess` | A fresh process refuses restart, and prior successful history stays preserved |
| `TestSurfaceCancellationAndRecovery` | Marker-before-fork race ends in recorded cancellation or recovery, with no silent restart |
| `TestTimeoutKillsProcessGroup`, `TestCancelKillsCheckAndRecordsCancellation` | Actual child absence or recovery, rather than absence of a marker scheduled 30 seconds later |

Initial verification used Go 1.27.3 on macOS arm64. The old-code nonzero-exit regression failed; all new acceptance tests passed after the fix. The runner suite passed with race detection. A 100-run surface cancellation test with two owned CPU-load processes passed; the load processes were terminated and waited for by the harness. This is stress evidence, not proof that the fork race cannot occur.

Independent read-only review found no production blockers. Two test issues were corrected before final validation: a child now records its own PID to permit cleanup if its parent dies, and timeout drains have their own bounds. Static analysis additionally identified test-fixture path review findings and an unchecked drain error; the paths received local rationale and the drain error is logged. Subsequent analysis completed with zero findings.

## Completion evidence and limits

The public acceptance tests are frozen in [the task record](tasks/NARU-033.json). Completion authority is the committed implementation and task-gate verification/closure, not this prose. Detailed commands, raw logs, analysis findings and commit references are kept in the local task evidence and project Wiki. The gate reruns format, native vet, race tests, Windows/Linux vet, build, and self-check including fresh static/vulnerability analysis on an isolated committed snapshot.

Minimum Go 1.23 and Linux/Windows runtime behavior were not tested here. Cross-platform vet is static evidence only. Processes escaping the group are outside this guarantee. Reused group IDs or slow zombie reaping can conservatively require recovery; inspection never kills the newly observed group. No actual push integration test, installation, release or repository-setting change is part of this task.
