# Changelog

All notable changes to naru are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change behaviour).

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

[0.1.0]: https://github.com/Chaebin-Park/naru/releases/tag/v0.1.0
