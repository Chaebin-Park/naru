# Execution records and recovery

NARU-006 uses schema 2 for new executions. Configuration remains version 1. Existing schema 1 receipts are preserved in place; they are never upgraded by inventing after-input evidence. Downgrading to an older binary is unsupported because it can reuse an old success while ignoring newer cancellation or interruption state.

## Files and execution order

| Path | Meaning |
| --- | --- |
| `.naru/state/checks/<check>.json` | Single authoritative current execution, running or terminal |
| `.naru/state/history/<check>/<run_id>.json` | Immutable copy of a previous current record |
| `.naru/state/locks/<check>.lock` | Permanent OS lock file; never unlink it to unlock a check |
| `.naru/logs/<check>-<run_id>.log` | Exclusive new log for one random 128-bit execution ID |
| `.naru/receipts/<check>.json` | Read-only legacy evidence |

The runner validates the request, takes a nonblocking per-check lock, reloads configuration, hashes inputs, prepares a unique log, archives prior state, and publishes `running` **before** starting the shell. Completion reloads configuration and hashes inputs again, closes/syncs the log, then publishes a terminal record. Cancellation after start writes `cancelled`; cancellation before start leaves existing evidence unchanged. A duplicate request returns 1 without replacing the owner or its history.

New state uses temporary files, checked writes, file sync, close, directory preparation sync on Unix, and an atomic final rename. All fallible preparation precedes publication, so a reported preparation error cannot publish terminal success. The final renamed directory entry is **not** claimed durable across physical power loss. Tests cover process termination and injected write/sync/close/directory-sync/rename failures, not power failure or network filesystems. Non-Unix directory sync and Windows runtime replacement behavior remain unverified.

## Validity and compatibility

Schema 2 requires explicit nullable fields and rejects missing keys, unexpected nulls, unknown keys, duplicate JSON keys, trailing JSON, incorrect check/run identity, unsupported contracts, and inconsistent result/input combinations. The command result, provenance label, execution phase, and input validity are separate fields. Provenance does not authenticate who ran the command.

A check passes only when its record is terminal, the command result is `pass`, input validity is `matched`, and the current hash and file count still match. The digest algorithm remains unchanged: selected paths, content, executable bits, symlink target strings, and `run/workdir/inputs/exclude`, plus declared nonempty `requires` (sorted lists). Timeout, tool/environment versions, and symlink target contents are not included.

Before/after comparison detects changes observed at those two boundaries, including a reloaded command/input definition. It is not a snapshot: A→B→A changes and changes during a multi-file read can escape detection. Checks that mutate their own inputs need a clean rerun. A zero-exit command can produce `input-changed` or `input-error`; this is not a successful check.

A structurally valid schema 1 receipt remains readable with its original result and source. A matching legacy success is `incompatible` (`policy_mismatch`), because it has no after evidence. A different current hash is `stale`. Invalid old evidence is `invalid`. When a schema 2 state file exists, even corrupt or unsupported state prevents fallback to schema 1. Special files and symlinked evidence paths are rejected; this is not protection against a hostile local process changing paths concurrently.

## Status and exit codes

`status --json` is schema 2. It retains `mode`, `satisfied`, and `checks`, and adds `unverified: {present, reason}`. Each row includes `check`, `state`, `reason`, `message`, `run_id`, `current_hash`, `provenance`, and optional `record` (schema 2) or `receipt` (legacy). Missing run IDs and provenance are null. `satisfied` describes the selected checks; it does not erase NOT VERIFIED state.

| State | Meaning |
| --- | --- |
| `pass`, `fail`, `stale`, `missing`, `empty` | Passed, failed, changed since the run, never run, or no input files |
| `running` | Another execution holds the check lock |
| `interrupted` | A running record remains after its owner disappeared |
| `cancelled` | A started execution recorded caller cancellation |
| `invalid`, `incompatible` | Evidence is corrupt/inconsistent or does not meet the current contract |
| `input-changed`, `input-error` | Execution inputs changed or could not be verified |

`status` returns 1 for structured unsatisfied states (including corrupt evidence), 64 for configuration/usage errors, and 70 for IO errors. `check` returns 1 for failed commands, duplicates, input mismatch, or recovery-required state; 64 for request/configuration/empty-input errors; 70 for storage/internal errors; and 130 for caller cancellation. Stop and pre-push use this same evaluator while retaining their mode/NOT VERIFIED policies.

## Interrupted owners and manual recovery

A free lock does not prove that a crashed owner's children have exited. A leftover `running` record is therefore reported as interrupted and **blocks automatic restart**. A terminal `owner_lost` reason also blocks restart: this includes a child holding output open past the wait limit, and Windows cancellation/timeout where descendant termination is not established. The command's original result remains visible; it is not accepted as verified.

For recovery, a human must first inspect and terminate the surviving processes belonging to that check. Do not kill an arbitrary PID from stale metadata. Preserve the affected `.naru/state/checks/<check>.json` outside the current-state directory, then rerun the check. Use the same preserve-and-move procedure for corrupt state after inspecting the cause. Do not delete the lock file. NARU does not automate this ownership decision or provide a recovery subcommand yet. Old history and schema 1 files remain unchanged.

On macOS/Linux, normal cancellation kills the shell's process group. Processes that deliberately detach, close inherited output, or escape the process group are outside that lifecycle guarantee. Run finite, trusted foreground checks. Existing background-command behavior must not be taken as a process sandbox. Windows still requires `sh`; its lock adapter has been cross-checked by vet, but Windows execution and descendant management are not verified. Separate working-copy roots and different checks have independent locks; symlink aliases of one root share the same lock. Network/distributed filesystems, external state deletion, and mixed old/new binaries are unsupported.

Implementation references: [flock handle lifetime](https://man7.org/linux/man-pages/man2/flock.2.html), [LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex), [Go process execution](https://pkg.go.dev/os/exec#Cmd).

## Unavailable prerequisites

After locking and reloading configuration, the runner inspects `requires.os`, implicit `sh`, and `requires.tools`. It does not execute probes. When unavailable, it archives prior state and publishes terminal `not-run` before log I/O. A log failure returns 70 but cannot restore a previous successful record. Input hashing and command execution are skipped.

Schema 2 adds optional `execution.unavailable: {reason, subject}` with reason `missing_tool` or `unsupported_platform`. A `not-run` record has null exit code, zero duration, equal start/finish times, input validity `unobserved`, an empty before digest and null after digest/reason. Other results cannot carry `unavailable`. Earlier schema 2 records remain readable; older binaries may reject these new values and are not supported for downgrade.

`status` reports `not-run` for saved unavailable records, or when the current prerequisites are missing and no running or invalid record takes precedence. A saved unavailable record remains unsatisfied after tools return; only a new successful run clears it. A fresh project without evidence is `missing` only when its prerequisites are available. `check` returns 1 for unavailable prerequisites; malformed declarations return 64. Status, Stop, and pre-push retain their existing policy and exit-code mappings.

Executable presence is not tool-version, service-health, credential, or connectivity verification. There is no automatic installation or remote execution. The environment can change after inspection. Tests: `TestUnavailableEnvironmentLifecycle`, `TestUnsupportedPlatformDoesNotRun`, `TestNotRunRecordValidation`, `TestUnavailableLogFailureCannotRestorePriorPass`, and `TestUnavailableRequirementsCLI`.
