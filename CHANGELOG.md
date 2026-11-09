# Changelog

Terms: [NARU glossary](docs/glossary.md). Command names and quoted session output keep their original spelling.

All notable changes to naru are recorded here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change behaviour).

## [Unreleased]

### Added

- Add native Windows CI for executable/project paths with spaces and Korean text, including missing/pass/stale/fail/recovery receipt checks.
- Extend Windows acceptance CI with process-handle observations across cancellation/timeout, inspected recovery and opt-in actual local push/ref checks.

## [0.2.0] - 2026-11-06

### Changed

- Isolate candidate builds from ignored source, live edits, archive attributes, Git replacement objects and ambient Go overlays/workspaces; add regression coverage for revision attribution.

- Document isolated candidate installation, migration and explicit platform limits; add source-bound cross-builds and SHA256 manifests without publishing.

- Add a clean-source public Web CI smoke runner and pinned GitHub Actions workflow; verify actual remote failure/recovery on Ubuntu with Go 1.23.12.

- Add a reproducible 10,000-file/100MiB status workload and macOS CLI latency/RSS measurement tool; document bounded Linux execution evidence and remaining runtime gaps.

- Add opt-in `mode_policy` requirements to shared status and gate decisions, with private findings reduced to counts and evidence digests.

- Add opt-in `mode_surfaces` requirements shared by status, Stop and pre-push. Extended reports use schema 3; check-only reports retain schema 2.

- Add `status --markdown` to render the common status verdict, reasons, provenance and NOT VERIFIED warning as a shareable table.

- Evaluate relay validity at one captured instant per status report.

- New executions use strict schema 2 state with immutable history, per-check OS locks, recorded cancellation, and before/after input verification. Legacy receipts are preserved read-only and require rerunning before they can satisfy the new policy.
- `status --json` is schema 2 with execution IDs, reasons, provenance, and NOT VERIFIED presence. Corrupt evidence now returns a structured invalid row and exit 1; IO errors return 70.
- Interrupted owners and unaccounted-for background children block automatic restart pending manual recovery. Windows runtime/process-tree support and physical power-loss durability remain unverified.

### Fixed
- Reject symlink ancestors before copying Android and Web sample sources into fresh build directories.

- Block check and extractor restart when a Unix process group remains after cancellation, timeout or other exit, including when an exit error hides the output wait limit. Preserve cancellation exit 130 and require manual recovery when cleanup cannot be confirmed.

- Block Stop when NOT VERIFIED state cannot be saved or cleared, and reject failed allowance notices.
- Reject log write/sync/close failures before publishing a receipt; remove temporary receipts after failed replacements.
- Report output failures on otherwise successful CLI commands and create private execution state with owner-only permissions.

### Documentation

- Record the current CLI, receipt format, verification boundaries, and known safety gaps; add regression coverage for mode/source precedence, corrupt receipts, and schema 1 field preservation.

- Define MVP support priorities: macOS and Linux validation first, with Windows limitations and untested behavior stated explicitly.

- Defer waivers beyond the MVP and retain the existing NOT VERIFIED completion policy.

- Add a shared glossary and align Korean terminology across current feature and design documents.

### Added
- Bounded Node Web pack for shared runtime routes, local workspace lock dependencies and environment key names, with offline isolated npm preparation and strict failure handling. No registry runtime dependency or core Go behavior change.
- Bounded Android public-example pack for debug merged-manifest and resolved-dependency changes, with fresh isolated builds, strict exports and explicit tool/SDK requirements. No emulator, device operation or core Go behavior change.
- Input-bound human relay requests and strict artifact-backed submissions with separate schema 3 provenance, expiring single-check authority, replay rejection and independent CI reruns. Request issuance supersedes previous evidence; no remote command or transfer is performed.
- Codex `apply_patch` protection for policy/config/evidence paths, shared hook payload acceptance tests, and inactive Claude Code/Codex connection templates with installation/removal guidance. Recorded Claude Code and Codex live missing/pass, stale/recheck and NOT VERIFIED paths, including headless workspace-trust limitations.
- Thin policy index linking existing rules, rationale and executable fixtures; complete known/new/resolved classification, terminal-only initial bootstrap and shrink-only approval, with independent policy storage and fresh candidate revalidation. No implicit Stop/pre-push integration.
- Bounded Spring Boot 3.5.16 public-example pack with profile-specific API/routes, SQL hashes, configuration-key extraction, fresh build exports and synthetic failure fixtures. Requires external Java/Maven/Python; no new Go dependencies or automatic activation.
- Terminal-only `baseline approve` with exact-preview and input revalidation, explicit empty-result confirmation, atomic snapshot/review metadata publication, and conditional legacy snapshot compatibility. Reviewer identity is self-reported; macOS/Linux terminal support does not provide authentication.
- `baseline extract` and `baseline diff` with bounded extractor execution, before/after input verification, separate candidate authority, validated snapshots and deterministic item changes. Extraction keeps baselines read-only; the separate approval command publishes them.
- Repository-local pack manifests with namespaced checks/surfaces, restrictive requirement merging, bounded symlink-free loading and pack-content fingerprints.
- Internal surface extraction schema and deterministic JSON canonicalization with strict validation and synthetic fixtures. Used by pack loading, candidate extraction, comparison and approval.
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
