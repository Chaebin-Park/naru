# Task completion and commit gate

This repository-local development tool prevents its task transition commands from starting a successor before the active task is verified and committed. It is not a released `naru` subcommand and never pushes, installs tools, or rewrites history.

## Commands

Run from the repository using an approved Go toolchain on PATH:

```sh
sh scripts/task-gate.sh check --next NARU-007 --json
sh scripts/task-gate.sh resume --task NARU-006 --json
sh scripts/task-gate.sh verify --task NARU-006 --json
sh scripts/task-gate.sh close --task NARU-006 --json
sh scripts/task-gate.sh begin --task NARU-007 --plan /path/to/public-plan.json --json
```

`check` is advisory and does not start work. `begin` repeats the checks under a worktree-local OS lock and commits the new plan/active record. `resume` allows unfinished modifications only for the already-active task; it does not allow switching tasks. `verify` tests a committed implementation in a temporary clone. `close` requires matching successful evidence and commits only the workflow and completion record. **Begin, close and one-time bootstrap create local metadata commits.** Implementation commits remain explicit agent/user actions.

A task has a start commit, one or more implementation commits, and a completion commit. Every implementation commit must have exactly one `Task: NARU-xxx` trailer matching the active task. Start/completion commits additionally have `Task-Action: begin` or `Task-Action: complete`. The completion commit's parent is the verified implementation SHA; completion commits may not contain product changes. Empty implementation commits do not satisfy completion.

## Public plan and authoritative records

Example plan input (use real test names for the actual task):

```json
{
  "title": "Handle unavailable check environments",
  "profile": "code-v1",
  "criteria": [
    {
      "id": "AC-1",
      "description": "Unavailable requirements cannot reuse a previous pass",
      "test": "github.com/Chaebin-Park/naru/internal/runner/TestUnavailableEnvironmentLifecycle"
    }
  ],
  "limitations": ["Tool availability does not establish service readiness."]
}
```

`docs/tasks/workflow.json` identifies the active and last completed tasks. `docs/tasks/<id>.json` contains the frozen plan, predecessor, base SHA, status, implementation SHA and original evidence digest. The gate reads committed Git objects. Missing, duplicate, null, unknown or inconsistent fields fail closed. Product implementation commits cannot modify these records; acceptance criteria cannot be relaxed in the middle of a task. Wiki prose explains decisions but is not parsed as machine evidence. Keep private paths and context out of public plans.

Version 1 deliberately uses only `code-v1`, even for documentation tasks. It runs formatting, native vet, full race tests with JSON events, Windows/Linux vet, a build, and the built binary's `naru check`, then inspects the fresh analysis report. Required criterion tests must emit pass events; skipped or absent required tests do not count. The gate unsets actual-push test opt-ins in child verification environments. It copies already-installed local analysis tools into the isolated checkout; it does not run setup. Missing Go or tools blocks progress.

Evidence is local under `.naru/state/taskgate/`, keyed by implementation SHA and bound to task, tree, plan digest, profile, canonical worktree and log hashes. Test output and analysis logs retain tool/database information. Reverification removes prior success before running: failure or interruption cannot leave an older successful attestation usable. New implementation commits require fresh verification.

If evidence or logs are lost, `check` blocks. Run `verify` for the last completed task to regenerate evidence against its recorded implementation commit. This is revalidation, not a claim to reproduce the original log digest or timestamp; the original completion record stays unchanged.

## Return contract

The gate emits one JSON verdict on stdout. Callers must require **exit 0 and `allowed: true`**, with matching schema/action/task/HEAD. A previous allow cannot be reused; call `begin` to perform the actual transition. Missing, malformed or contradictory output is denial. Wrapper failures can emit only a minimal denial object; they can never grant permission.

| Exit | Meaning |
| --- | --- |
| 0 | The requested operation succeeded |
| 1 | A prerequisite is unmet; the next task is blocked |
| 64 | Invalid invocation |
| 70 | Inspection, verification execution, persistence or tool failure |
| 130 | Interrupted |

Common reasons include `PREVIOUS_TASK_INCOMPLETE`, `WORKTREE_DIRTY`, `COMMIT_MISSING`, `TASK_TRAILER_MISMATCH`, `PLAN_CHANGED`, `CLOSURE_INVALID`, `VERIFICATION_MISSING`, `VERIFICATION_STALE`, `ACCEPTANCE_PENDING`, and `TRANSITION_BUSY`. `next_action` explains the blocked condition. All nonzero exits stop task advancement.

## Recovery and boundaries

The gate does not stash, reset, delete or automatically commit product edits. A failed metadata commit leaves its generated files available for inspection and blocks further advancement. Inspect the task-file/index diff, resolve the Git failure, and preserve or restore only those known generated metadata changes before retrying the transition. Never clear the permanent lock file to unlock a running gate. Unexpected commits after closure, detached HEAD, merges, rebases and branch mismatches require explicit recovery; v1 supports a linear task history.

`bootstrap --task ID --plan file --json` is an explicit one-time migration. It refuses any ledger history reachable from Git refs, including a ledger later deleted. It records a historical boundary and starts an active task; it does not attest that pre-gate work passed this protocol. Bootstrap requires a clean snapshot. An imported unfinished task stays unfinished and can only be resumed. Do not use bootstrap as a bypass.

The lock serializes cooperating gate invocations. External Git/file operations are checked again and cause denial if observed; this is not filesystem isolation against a hostile process. SHA-1 Git repositories are supported in v1. Ignored cache/log files do not block transitions; verification uses a clean clone so ignored local source cannot supply hidden test inputs. Submodule content is not automatically initialized by the verifier; missing build inputs fail verification.

The controlled task-transition entry point and agent instructions enforce this workflow. They do **not** intercept arbitrary file writes made outside that entry point, authenticate local evidence against its owner, or install an agent-host hook. Host tool interception and protected CI policies are separate integrations. Task labels and passing tests do not prove arbitrary acceptance prose; test selection and review remain engineering responsibilities.

The verifier uses Go process cancellation and bounded output waiting. It is not a process sandbox for malicious or deliberately detached child processes. Windows/Linux vet is static evidence, not runtime evidence. Actual push tests remain excluded unless separately authorized and deliberately run outside this default profile.
