# naru

> **naru** (나루) is Korean for a ferry crossing: changes cross only after the boatman checks them.

`naru` makes an AI coding agent's "done" mean *verified*, not *claimed*. It runs the checks your project requires, records a **receipt** bound to the exact content of the files each check depends on, and blocks the agent from finishing while any required check is missing, failing, or stale.

[한국어 README](README.ko.md)

> Status: early development (P0). The core loop works: `init`, `check`, `status`, `mode`, and the Claude Code Stop gate. Baselines, policies, relay, and platform packs are planned.

## Why

Agents often report "tests pass" without running them, or run them and then keep editing. A receipt that is tied to the inputs closes both gaps:

| Situation | `naru status` | Stop gate |
| --- | --- | --- |
| check never ran | `missing` | blocks |
| check ran and failed | `fail` | blocks |
| check passed, then an input file changed | `stale` | blocks |
| check passed for the current files | `pass` | allows |
| a check's inputs match no files | `empty` | blocks (a receipt would prove nothing) |

The gate never runs checks itself, so it stays fast; it only compares receipts with the current files.

## How it fits together

![naru architecture: agent adapters call one core CLI, which loads platform packs and records evidence](docs/images/naru-architecture.png)

![naru workflow: implementation loops until the Stop gate sees fresh passing receipts; baseline diffs and remote work go to a human](docs/images/naru-workflow.png)

The diagrams show the planned design; P0 implements the core CLI, receipts, modes, and the Stop gate. Diagram labels are partly in Korean.

## Install

Requires Go 1.23 or newer.

```sh
go install github.com/Chaebin-Park/naru/cmd/naru@latest
```

## Quick start

```sh
cd your-repo
naru init          # writes .naru/config.yaml (detects Go, Gradle, Maven, SwiftPM, Node, Python, Rust)
naru check         # runs the checks of the current mode and records receipts
naru status        # pass / fail / stale / missing for each required check
```

`.naru/config.yaml`:

```yaml
version: 1
default_mode: code
modes:
  code: [test, lint]
  docs: [links]
checks:
  test:
    run: "./gradlew test"
    inputs: ["**/*.kt", "**/*.kts", "gradle/**"]   # receipts are bound to these files
    timeout: 15m
  lint:
    run: "./gradlew detekt"
    inputs: ["**/*.kt", "config/detekt/**"]
  links:
    run: "lychee docs/"
    inputs: ["docs/**"]
exclude: ["build/**"]
```

- `inputs` are root-relative globs. `*` and `?` stay within one path segment; `**` matches any depth (`**/*.go` includes root files). Matching is case-sensitive. Braces, brackets, absolute paths, `..` and backslashes are rejected rather than silently matching nothing.
- `naru init` uses `inputs: ["**"]` plus stack-specific `exclude` entries. Narrow inputs only when a check truly ignores the rest: an over-broad input costs a re-run, a missing one lets stale work pass.
- Inside git, files ignored by `.gitignore` never count, and submodules and nested repositories are hashed by content. Outside git, or when the enclosing repository ignores the project directory, the tree is walked. `.naru/` and every `.git` are always excluded. Files are hashed by content and executable bit; symlinks by their target.
- Changing a check's `run`, `workdir` or `inputs`, or the global `exclude`, invalidates its receipt.
- Every mode must require at least one check. Unknown keys are rejected, so typos fail loudly.

## Modes

A mode is the set of checks that must pass before an agent may finish. The active mode is chosen by `--mode`, then `$NARU_MODE`, then `naru mode <name>` (stored per working copy), then `default_mode`.

```sh
naru mode docs     # a docs-only task should not need the full test suite
naru mode --clear
```

## Agent integration

### Claude Code

Add a Stop hook to `.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "naru gate stop" }] }
    ]
  }
}
```

When the gate blocks, it exits 2 and explains on stderr which checks are not satisfied and what to run; Claude Code feeds that back to the model and keeps working.

- The project is found from `$CLAUDE_PROJECT_DIR` first, then the hook payload's `cwd`, so an agent that `cd`s elsewhere is still gated. Outside a naru project the gate always allows.
- An invalid config blocks, because an unverifiable state is not a passing one.
- When the active mode is not `default_mode`, an allowed stop says so on stderr, so a narrowed mode is visible in the session.
- Escape hatch for humans: set `NARU_GATE=off` in the environment Claude Code runs hooks with. Every stop then reports that completion was not verified.

### Codex and others

Native hook support is planned. Until then, add to `AGENTS.md`:

```md
Before reporting completion, run `naru status`. If it exits non-zero, run `naru check`, fix failures, and repeat.
```

## Receipts

`naru check` writes `.naru/receipts/<check>.json` (latest run) and `.naru/logs/<check>-<time>.log`. The receipt records the command, the inputs hash and file count, the git HEAD, result and exit code, timing, and an evidence class (`local`, `agent-run`, or `ci`; set with `--evidence` or `$NARU_EVIDENCE`, and `ci` when `$CI` is set). `naru init` adds `.naru/.gitignore` so receipts, logs, and the stored mode stay out of version control.

## Exit codes

| Command | 0 | 1 | 2 | 64 | 70 |
| --- | --- | --- | --- | --- | --- |
| `check`, `status` | done | not done | – | usage or config error | internal error |

`check` exits 130 when interrupted (Ctrl-C stops the whole check process group and writes no receipt).
| `gate stop` | allow | – | block | usage error | – |

## Known limitations (P0)

- Receipts and config are local files. An agent that edits `.naru/` directly (receipts, `run: true`, or `naru mode`) can get past the gate; check-definition changes invalidate receipts and narrowed modes are reported, but prevention needs the planned PreToolUse guard for `.naru/`.
- Checks run sequentially with `sh -c`. Windows needs a POSIX shell (for example Git Bash), and a timeout there kills only the shell, not its children.
- If a check leaves a background process holding its output open, naru records the exit status and stops waiting after 5 seconds.
- No baselines, policy index, relay, or platform packs yet.

## Roadmap

P0 core loop → P1 baseline engine and Spring Boot pack → P2 Claude Code plugin, git hooks, relay, waivers → P3 Android and Web packs → P4 knowledge MCP and evals → P5 iOS pack, CI templates (GitHub Actions, Bitbucket Pipelines, Jenkins), review dashboard.

## License

[MIT](LICENSE)
