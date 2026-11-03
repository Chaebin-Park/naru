# AGENTS.md — working on naru

naru gates an AI coding agent's "done" on receipts bound to the files its checks actually saw. These rules apply to any agent (Claude Code, Codex, others) editing this repository.

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

- Safety first: the gate must never report pass for files that differ from what was checked. When unsure, fail closed and explain.
- Every bug fix gets a regression test that fails without the fix. Behaviour seen in a live agent session goes into `docs/verification.md`.
- Keep the agent-facing output short and actionable: what is wrong, what to run.
- Code, comments, CLI messages and README are English; keep `README.ko.md` in sync for user-facing changes.
- Record user-visible changes under `## [Unreleased]` in `CHANGELOG.md`.
- Commits: imperative subject with an optional `feat:` / `fix:` / `docs:` / `chore:` prefix; explain why in the body.
- Never commit `diagrams/` (local sources with third-party logos), receipts, logs, or anything identifying a company or private project.
- Ask the maintainer before pushing, tagging, publishing a release, or changing repository settings.
