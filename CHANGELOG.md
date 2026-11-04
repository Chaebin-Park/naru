# Changelog

Terms: [NARU glossary](docs/glossary.md). Command names and quoted session output keep their original spelling.

All notable changes to naru are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change behaviour).

## [Unreleased]

### Changed

- New executions use strict schema 2 state with immutable history, per-check OS locks, recorded cancellation, and before/after input verification. Legacy receipts are preserved read-only and require rerunning before they can satisfy the new policy.
- `status --json` is schema 2 with execution IDs, reasons, provenance, and NOT VERIFIED presence. Corrupt evidence now returns a structured invalid row and exit 1; IO errors return 70.
- Interrupted owners and unaccounted-for background children block automatic restart pending manual recovery. Windows runtime/process-tree support and physical power-loss durability remain unverified.

### Fixed

- Block Stop when NOT VERIFIED state cannot be saved or cleared, and reject failed allowance notices.
- Reject log write/sync/close failures before publishing a receipt; remove temporary receipts after failed replacements.
- Report output failures on otherwise successful CLI commands and create private execution state with owner-only permissions.

### Documentation

- Record the current CLI, receipt format, verification boundaries, and known safety gaps; add regression coverage for mode/source precedence, corrupt receipts, and schema 1 field preservation.

- Define MVP support priorities: macOS and Linux validation first, with Windows limitations and untested behavior stated explicitly.

- Defer waivers beyond the MVP and retain the existing NOT VERIFIED completion policy.

- Add a shared glossary and align Korean terminology across current feature and design documents.

### Added
- Per-check `requires.tools` and `requires.os`, with persisted `not-run` reasons, implicit shell availability checks, and explicit rerun after environment recovery. No tool installation, version probes, or service health checks.
- Development task gate with committed task records, isolated verification, required test evidence, and fail-closed begin/resume/close transitions. This does not install agent-host interception hooks.
- Repository-local development scanning with pinned golangci-lint/govulncheck tools, foreground watch mode, structured local issue reports, and a required analysis check. This is development tooling, not a released naru subcommand.
- `naru gate pre-push`: require fresh passing receipts for `default_mode` and no NOT VERIFIED record, independently of Stop overrides.
- `naru hooks install [--force]`: opt-in Git pre-push hook installation with existing-hook protection and hook-manager integration guidance.
- `AGENTS.md`: build, verification and contribution rules for coding agents working on this repository.

## [0.1.0] - 2026-11-03

First release: the core loop that gates an AI agent's "done" on receipts.

### Added
- `naru init` detects Go, Gradle, Maven, SwiftPM, Node, Python and Rust and writes `.naru/config.yaml` (inputs `**` plus stack excludes; fails closed when no build file is found).
- `naru check` runs checks with `sh -c`, timeouts that kill the whole process group, Ctrl-C handling, and atomic receipts bound to the content of each check's inputs and its full definition.
- `naru status` reports `pass`, `fail`, `stale`, `missing` or `empty` per check (`--json` available).
- `naru mode` selects which checks a task requires; every mode must require at least one check.
- `naru gate stop` (Claude Code Stop hook) blocks completion until the current files pass, finds the project via `$CLAUDE_PROJECT_DIR`, and stops endless block loops after `gate.max_repeats` identical blocks (default 3), reporting **NOT VERIFIED** to the user and in `naru status`.
- `naru gate pre-tool` (Claude Code PreToolUse hook) blocks agent writes to `.naru/` and agent mode changes.
- `NARU_GATE=off` escape hatch for humans.

### Known limitations
- The PreToolUse shell detection is a heuristic, not a security boundary; re-run checks in CI.
- Windows needs a POSIX `sh`, and timeouts there do not kill child processes.
- No baselines, policy index, relay, git hooks or CI templates yet.

[Unreleased]: https://github.com/Chaebin-Park/naru/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Chaebin-Park/naru/releases/tag/v0.1.0
