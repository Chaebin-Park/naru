# naru

[한국어](README.ko.md)

naru runs repository checks and records which inputs each result applies to. A completion or pre-push gate can then require fresh passing receipts for the current files.

## Install and get started

Install the [latest published version](https://github.com/Chaebin-Park/naru/releases/latest) with Go 1.23 or newer. Run the remaining commands from the project directory:

```sh
go install github.com/Chaebin-Park/naru/cmd/naru@latest
naru init
naru check
naru status
```

Review `.naru/config.yaml` after initialization. Define each check's command and input globs so that a change to any file the command relies on invalidates its receipt. Include build configuration and dependency manifests where applicable. A mode selects the required checks. Run `naru mode <name>` to change the mode for this working copy.

## Read the result and enforce it

| Result | Action |
| --- | --- |
| Pass | The receipt matches the current declared inputs and check definition. |
| Missing or stale | Run the required checks again. |
| Fail | Inspect the check output, correct the failure and rerun it. |
| Invalid or interrupted | Inspect the execution records and follow the recovery procedure. |

Use `naru status --json` for automation or `naru status --markdown` for a report. The two flags are mutually exclusive. The gate also blocks on unsatisfied requirements such as NOT VERIFIED. The gate evaluates existing evidence. Run `naru check` to generate new evidence.

Use the [Claude Code and Codex examples](examples/agent-hooks/README.md) to configure hooks for your agent. Confirm hook execution with a missing receipt and again after a passing check. Git hooks can use `naru gate pre-push`. CI should run its own checks against its own checkout.

## Project checks and recovery

- Use the repository-local packs for Android, Spring Boot or Web as examples. Each pack requires its own build tools and has documented input and export limits. [Packs](docs/packs.md)
- Inspect candidate surface diffs before approving a baseline. Human-relayed checks retain their distinct evidence source and validity conditions. [Baselines](docs/baselines.md) · [Relay](docs/relay.md)
- Keep local check logs and receipts out of version control. If a check is interrupted, follow the recovery procedure before clearing state. A child process can survive cancellation on Windows. [Execution records and recovery](docs/execution-records.md)

## Downloads and support

Download the archive for your operating system and architecture from the release page. Compare its SHA-256 digest with the release's `SHA256SUMS` before extracting it. For an attested release, verify the artifact's workflow, tag and source commit using the release trust policy. [Releases](https://github.com/Chaebin-Park/naru/releases) · [Release trust policy](docs/release-trust.md)

Archives are built for macOS arm64/amd64, Linux arm64/amd64 and Windows amd64. Native tests cover specific workflows. Check the platform evidence before relying on a particular shell, cancellation behavior or pack. The Linux benchmarks apply to the tested workloads and configurations. In the later isolated run, the 10,000-file synthetic workload missed the 2-second warm-cache p95 target. [Platform verification](https://github.com/Chaebin-Park/naru/actions) · [Measurement results](docs/linux-status-measurement.md)

## Contribute

See the [contribution guide](CONTRIBUTING.md) and [changelog](CHANGELOG.md). The project is distributed under the [MIT License](LICENSE).
