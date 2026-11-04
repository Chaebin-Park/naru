# Implementation rules

These rules apply to implementation work in this repository. [AGENTS.md](AGENTS.md) is the agent entry point. Terms are defined in the [glossary](docs/glossary.md).

## Task and commit boundaries

Before editing, record the problem, intended behavior, scope, exclusions, acceptance criteria, and relevant tests in the task record. Inspect the working tree and distinguish existing changes from this task. Do not discard or silently include someone else's changes.

One commit has one reason to change and one coherent rollback boundary. A task can need several commits; a file can participate in more than one task.

- Include the implementation, tests that prove its behavior, and directly affected usage or compatibility documentation together.
- Separate unrelated formatting, renames, refactoring, dependency changes, and feature work. A prerequisite refactor gets its own passing commit when it can stand alone.
- Keep each commit buildable and its applicable tests passing. Do not commit a deliberately failing test separately from the fix. During development, first observe that the regression test fails for the intended reason.
- As a review aid, aim for roughly 100–300 changed implementation lines. This is a prompt to review scope, not a limit or a reason to split an atomic change. Explain larger commits in the task record; fixtures and generated data are assessed separately.
- Internal preparation can precede activation, but it must serve the documented next step and have meaningful tests. Do not expose a partially safe execution path or add speculative APIs.
- A revert may require reverting dependent later commits first. Record dependencies; do not claim every commit can be reverted in arbitrary order.

Prefer an imperative English subject: `type(scope): action`, with `feat`, `fix`, `refactor`, `test`, `docs`, or `chore`. Scope is optional. Add a body for the reason, compatibility impact, or non-obvious tradeoff. Link the task with a trailer such as `Task: NARU-005.2`. Commit text is public: no private vault paths or identifying project details.

Before committing, inspect `git diff --cached` and `git diff --cached --check`. Stage explicit files or hunks; do not use a broad add to collect unrelated work. The commit must not rely on an unstaged file to build or pass its relevant tests. When the working tree mixes tasks, verify the proposed staged snapshot in an isolated temporary checkout before committing it. Preserve the user's working tree and index while doing so.

Do not rewrite or amend existing commits as routine cleanup. Do not infer permission to push from permission to commit. Pushes (including local test remotes), tags, releases, repository-setting changes, and system installation require prior maintainer approval. Ordinary task documentation does not itself create a commit.

## Go conventions

Follow nearby code and `gofmt`. Keep compatibility with the `go.mod` language/toolchain minimum (currently Go 1.23); testing with a newer toolchain alone does not prove minimum-version compatibility. Do not add language features or APIs above that minimum without an explicit compatibility decision.

- Use short, descriptive lowercase package names and Go naming conventions such as `runID`, `fileCount`, and `JSON`. Export only what another package needs. Avoid generic `utils` packages.
- Keep CLI parsing and exit-code mapping in `internal/cli`; execution and process lifetime in `runner`; persistence and decoding in `receipt`; evaluation in `status`; hook-specific decisions in `gate`. Put platform-specific behavior in narrowly scoped files with build constraints.
- Prefer concrete types and ordinary functions. Introduce a small interface at the consuming boundary only when it serves a real alternative or meaningful fault injection. Avoid one-interface-per-struct designs.
- Keep results, provenance, input validity, and completion policy distinct. Use named constants for finite states. Validate external data before it enters the evaluator; zero values must not manufacture successful evidence.
- Return errors from internal packages. Add useful operation context with `%w`; inspect wrapped errors with `errors.Is` or `errors.As`. Do not both print and return the same error at several layers. Do not use panic or `os.Exit` for expected failures inside internal packages.
- Pass cancellation through `context.Context`. Release files, locks, and processes on every path. Check persistence and completion errors; never turn an ignored write/close/sync error into success. Do not silently swallow cleanup failures that can affect correctness.
- Use `filepath` for OS paths and explicit slash conversion for stored portable paths. Validate names used in paths. Do not interpolate untrusted strings into shell commands. User-configured check commands retain the documented `sh -c` contract.
- Prefer local state over mutable package globals. Inject clocks, IDs, and IO only where tests or correctness require control. Keep test-only hooks narrow.
- Write code, comments, diagnostics, and public developer docs in English. Comments explain intent, invariants, and constraints; exported APIs have useful doc comments. Keep Korean user documentation in sync.

Use existing dependencies and the standard library first. New dependencies, toolchain upgrades, lint tools, and automatic format hooks are separate decisions, not incidental setup. Do not install tools or alter repository settings to enforce these rules without prior approval.

## Tests and verification

Test observable behavior and safety boundaries, not private implementation details. Use table tests for related cases, `t.TempDir` for files, `t.Setenv` for environment, and `t.Cleanup` for cleanup. Do not parallelize tests that mutate shared globals, process state, or the environment.

For bug fixes, reproduce the failure before changing the implementation. For new behavior, write acceptance cases alongside the implementation. For behavior-preserving refactors, establish regression coverage first when it is missing. Do not add tests solely to mirror trivial code or inflate coverage.

- Persistence: cover missing, malformed, unsupported, partial, and inconsistent records; verify failure never becomes pass.
- Concurrency: use real subprocesses for process-level locks, and barriers with bounded timeouts instead of timing-only sleeps. Prove the first execution remains intact when another is refused.
- Cancellation/recovery: verify behavior from a fresh process, including old successful evidence and leftover child processes.
- CLI: verify exit codes and essential output fields. Use full golden output only when the exact text or JSON is an intentional contract.
- Fixtures: use synthetic data with no private paths or secrets. Distinguish illustrative examples from valid evidence. Never copy live receipts or logs into fixtures.

Run `sh scripts/devcheck.sh` after each coherent code-edit batch and before committing; triage and report findings as described in [development analysis](docs/development-analysis.md). Do not infer current vulnerability status from cached receipts.

During a change, run the smallest relevant tests first. Before a code task is complete, run the full sequence in [AGENTS.md](AGENTS.md): format, vet, race tests, cross-platform vet, build, and the built binary's `naru check`. Keep actual push tests opt-in and obtain approval before running them. Do not narrow `.naru/config.yaml` to obtain a pass.

Repeat affected checks after further source changes. Final evidence must match the files being delivered. Verify the declared minimum Go version when a compatible toolchain is available; otherwise report the gap and avoid claiming it was tested. Cross-platform compilation/vet does not establish runtime support. A skipped test is not a passing scenario.

For documentation-only work, check links, examples, and whitespace; confirm that source files are unchanged. No application test rerun is needed unless the documentation changes an executable example or other runtime input. Record exactly what ran, what was reused, and what remains unverified.

## Documentation and completion

Update the task record as work proceeds: context → decision → implementation → verification → next step. Do not replace historical evidence with current claims. Public behavior changes update README and README.ko together and add an Unreleased changelog entry. Compatibility changes also update the behavior baseline. Live agent-session findings belong in `docs/verification.md`; ordinary unit test runs must not be labeled live agent evidence.

Before marking a task complete:

1. Re-read the diff for scope, correctness, failure behavior, and unintended public/API changes.
2. Check acceptance criteria and record fresh evidence or explicit verification gaps.
3. Confirm docs describe implemented behavior separately from planned behavior.
4. Record deferred work with its reason and owning task. A required unfinished acceptance criterion keeps the task open.
5. Report changed behavior, verification, limitations, and commit status. Self-review is not independent review.

Keep private planning and company-specific context outside the public repository. Do not commit local diagrams, generated receipts/logs, or temporary artifacts. A task can be locally verified while an explicitly approval-gated integration test remains pending; report these statuses separately instead of calling the entire feature complete.
