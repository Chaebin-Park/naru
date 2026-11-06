# Installation and release preparation

This document describes unpublished candidates from the current repository. `go install ...@latest` installs the latest published module version, which may not contain these changes. No new tag or release is created by this workflow.

## Candidate preparation

Use an already installed Go 1.23+ toolchain, Python 3 and POSIX shell. From a clean committed checkout:

```sh
sh scripts/prepare-candidate.sh /tmp/naru-candidate-UNIQUE
cd /tmp/naru-candidate-UNIQUE
shasum -a 256 -c SHA256SUMS
./naru-darwin-arm64 version  # choose the binary for your actual host
```

The output directory must be new and absolute. The script refuses a dirty checkout or existing destination. It builds CGO-disabled macOS arm64/amd64, Linux amd64/arm64 and Windows amd64 executables with a `candidate-<revision>` version, records the full source revision and build toolchain, includes LICENSE, and hashes every artifact/metadata file. These are raw executable candidates, not signed installers. Output stays outside the repository; retain failed preparation output for diagnosis and use a new directory on retry.

A SHA256 sum detects changed bytes; it does not authenticate the publisher. Check the trusted source revision separately. Do not sign, tag, push or publish a release without maintainer approval. The checksum file does not hash itself.

## Isolated installation smoke

Copy the host candidate into a temporary `bin/naru`, add that directory to a command-scoped PATH, and create a separate synthetic Go module with a passing test. In that module, verify:

1. `naru version` reports the candidate revision.
2. `naru init` exits 0; a second init refuses overwrite (64).
3. `naru status` exits 1 with missing evidence.
4. `naru check` exits 0; `naru status --json` is satisfied and `naru gate pre-push` exits 0.
5. Change a declared input. Status and pre-push exit 1 until a successful recheck.
6. Make the test fail. Check exits 1 and status cannot reuse old passing evidence.
7. Restore the test and recheck; status returns 0.

This uses a temporary installation only. No shell profile, global binary directory or real repository hook is changed. Actual `git push` integration is a separate opt-in test.

## Platform and shell contract

| Target | Current evidence | Support boundary |
| --- | --- | --- |
| macOS arm64 | Native full checks and temporary candidate smoke | Requires POSIX `sh` and project-specific tools |
| Linux amd64 | Selected Rocky Linux container tests; actual Ubuntu/Go 1.23.12 Web CI | Full minimum-Go suite not run |
| macOS amd64, Linux arm64 | Candidate cross-build only | Runtime not verified |
| Windows amd64 | Cross-vet and candidate cross-build only | Runtime unverified; POSIX shell required; descendant cleanup not guaranteed |

See [reliability measurements and lifecycle limits](reliability-performance.md) and [remote CI evidence](ci-smoke.md). Cross-build success never implies runtime support. Tools/SDKs for Android, Web and Spring Boot packs are separate prerequisites; NARU does not install them.

## Upgrade and migration

Back up configuration and any reviewed baseline before upgrading. Read [execution records](execution-records.md), [common reports](common-report.md), [relay](relay.md) and [baseline approval](baselines.md). Do not hand-edit evidence to obtain a pass.

- Rerun checks after changing the binary or check definitions. Legacy execution records may remain readable without being sufficient current evidence; interrupted owners require the documented recovery procedure.
- Baseline snapshots without approval metadata do not establish an approved unchanged contract. Rebuild, inspect the diff and use interactive `baseline approve` where appropriate.
- Existing modes keep their check requirements. Add `mode_surfaces` or `mode_policy` explicitly only when those requirements are intended; missing/stale/busy evidence blocks selected modes.
- Check-only status JSON remains schema 2. Extended reports use schema 3. Consumers must inspect the schema and handle additional requirement rows; `--markdown` and `--json` are mutually exclusive.
- Relay success remains human-relayed evidence with validity limits. CI executes independently; do not migrate local receipts into CI as a substitute.
- Compare English/Korean README instructions and review Unreleased changes before distribution. Existing generated hook configuration is not silently replaced by a binary upgrade.

## Publication review checklist

Before asking to publish, record source revision, toolchain, artifact hashes, runtime results and missing platforms; inspect the candidate file list and public diff for credentials, private paths and company/project identifiers. Candidates contain binaries, LICENSE, revision/toolchain metadata and checksums only: no receipts, logs, private planning, diagram sources or workspace files. Review binary build metadata as well as source paths (`-trimpath` reduces local-path disclosure but is not a general secret scanner).

MVP acceptance and independent review are tracked separately in NARU-022. Candidate preparation is not permission to publish or evidence that all acceptance conditions have passed.
