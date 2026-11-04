# AGENTS.md — working on naru

Terms: [NARU glossary](docs/glossary.md). Command names and quoted session output keep their original spelling.

naru gates an AI coding agent's "done" on receipts bound to the files its checks actually saw. These rules apply to any agent (Claude Code, Codex, others) editing this repository.

## Implementation workflow

Follow [CONTRIBUTING.md](CONTRIBUTING.md) for task/commit boundaries, Go conventions, tests, staged-snapshot verification, and completion records. Keep one coherent change per commit, with its tests and directly affected documentation.

## Required task transition gate

Before any task implementation, read `docs/tasks/workflow.json` and run the gate:

- For the active task: `sh scripts/task-gate.sh resume --task <ID> --json`.
- For a new task: `sh scripts/task-gate.sh begin --task <ID> --plan <public-plan.json> --json`.
- Require process exit 0 AND valid JSON with `allowed: true` and the matching action/task/HEAD. Any missing output, nonzero return, or mismatch blocks the requested task. A prior `check` result is not reusable permission.
- If a predecessor is incomplete, resume that predecessor; do not start the next task, bypass the command, or edit task records to manufacture completion.
- Give implementation commits exactly one matching `Task: <ID>` trailer. After the implementation commits, run `verify --task <ID> --json`, then `close --task <ID> --json`. Both must succeed before moving on.
- `begin` and `close` create local metadata commits. They do not push. Keep private context in the Wiki and public acceptance/tests in the task plan.
- Missing Go or evidence is a blocker, not permission to skip the gate. Use the approved available toolchain; do not install one automatically.

See [task gate](docs/task-gate.md) for verification, return codes and recovery. This enforces the controlled task workflow; no agent-host tool interception hook is installed.

## Development analysis

Run `sh scripts/devcheck.sh` after each coherent code edit batch and before reporting completion or committing. For continuous local feedback, use `sh scripts/devcheck.sh -watch` in a foreground terminal. Read the reported `.naru/state/devcheck-*/issues.md` and surface new findings to the user with IDs and source locations. Do not silently suppress findings or call an incomplete scan clean. `naru check` also runs analysis; vulnerability results must be refreshed, not inferred from a cached `status` receipt. See [development analysis](docs/development-analysis.md) for pinned tools and report semantics.

## Build and verify

Go 1.23+ (module `github.com/Chaebin-Park/naru`, one dependency: `go.yaml.in/yaml/v3`).

```sh
gofmt -l .                      # must print nothing
go vet ./...
go test -race -count=1 ./...
GOOS=windows go vet ./... && GOOS=linux go vet ./...
go build -o /tmp/naru ./cmd/naru && /tmp/naru check   # naru dogfoods itself: fmt, vet, test
```

A change is done only when `naru check` passes for the current files (see `.naru/config.yaml`). Do not edit `.naru/` to make it pass.

## Layout

- `cmd/naru` — entry point.
- `internal/cli` — command dispatch; end-to-end tests (`cli_test.go`, `regression_test.go`, `pretool_test.go`).
- `internal/config` — `.naru/config.yaml` schema and validation.
- `internal/fileset` — file listing (git or walk) and input hashing.
- `internal/runner` — runs a check, writes logs and receipts.
- `internal/status` — pass / fail / stale / missing / empty evaluation.
- `internal/gate` — Claude Code hooks: `stop.go`, `breaker.go` (no-progress breaker), `pretool.go` (`.naru/` guard).
- `internal/mode`, `internal/receipt`, `internal/initcmd`.
- `docs/verification.md` — live Claude Code session evidence. `CHANGELOG.md` — Keep a Changelog.

## Rules

- Safety first: the gate must never report pass for files that differ from what was checked. If the result is uncertain, block the action and explain why.
- Every bug fix gets a regression test that fails without the fix. Behaviour seen in a live agent session goes into `docs/verification.md`.
- Keep the agent-facing output short and actionable: what is wrong, what to run.
- Code, comments, CLI messages and README are English; keep `README.ko.md` in sync for user-facing changes.
- Record user-visible changes under `## [Unreleased]` in `CHANGELOG.md`.
- Commits: imperative subject with an optional `feat:` / `fix:` / `docs:` / `chore:` prefix; explain why in the body.
- Never commit `diagrams/` (local sources with third-party logos), receipts, logs, or anything identifying a company or private project.
- Ask the maintainer before pushing, tagging, publishing a release, or changing repository settings.
