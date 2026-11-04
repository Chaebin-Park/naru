# Development analysis

This is a temporary Go development aid for this repository. It does not add a released naru command, implement the planned baseline/relay features, or publish GitHub issues.

## Setup and use

Put a compatible Go toolchain on the current shell's PATH. The application remains Go 1.23-compatible by policy; these **development tools** have higher requirements. The verified setup uses Go 1.27.3 on macOS arm64, golangci-lint 2.15.0 (built with Go 1.27.0), and govulncheck 1.9.0 (module minimum Go 1.26). Minimum application-toolchain compatibility is a separate check.

```sh
sh scripts/setup-devtools.sh      # explicit local download/build, no system installation
sh scripts/devcheck.sh            # one scan; findings exit 1, incomplete scan exits 70
sh scripts/devcheck.sh -watch     # foreground: scan on change, poll every 2 seconds
```

Setup downloads the pinned golangci-lint archive, verifies its published SHA256 checksum, and builds the pinned govulncheck into `.naru/state/devtools/bin`. It does not change the application module dependencies or shell profile. Go's normal module/build caches may be populated. Setup supports macOS/Linux; Windows setup/runtime is not verified. Nothing downloads automatically during a normal scan except the vulnerability database and ordinary Go module needs.

The wrapper builds a temporary Go executable and preserves its exit code. The watch process runs scans sequentially. It also refreshes hourly even without source changes, since the vulnerability database can change. It is not installed as a daemon or editor hook. Stop it with Ctrl-C; an agent's session ending does not guarantee continued watching. Different invocations may overlap; golangci-lint's own process lock can refuse one, which is reported as an incomplete scan, not a clean scan.

## Checks and feedback

`.golangci.yml` explicitly enables govet, staticcheck, errcheck, ineffassign, unused, and gosec. Tests are included; preset exclusions and per-rule issue-count limits are disabled. govulncheck includes test code and analyzes symbols. Every code-edit batch must run the scanner, inspect its report, and surface new issues with source locations. See [AGENTS.md](../AGENTS.md).

The repository's `code` mode now includes `analysis`, invoking the same scanner. Its declared inputs include Go files, module files, the lint config, and the wrapper. Existing findings cause the analysis check to fail. Do not disable rules or exclude files just to make the gate pass.

Reports live in a unique ignored directory per scan:

```text
.naru/state/devcheck-<unique>/
  report.json           # timestamps, input hash, completeness, tool versions, issues/errors
  issues.md             # readable issue list
  golangci-lint.json    # raw tool output
  golangci-lint.stderr
  govulncheck.json      # includes scanner/database metadata
  govulncheck.stderr
```

The terminal prints the report path and each finding's ID, priority, rule, location, and message. IDs are derived from tool/rule/location/message, so identical findings repeat with the same ID; line movement can change an ID. These are scan snapshots, not a full issue-tracker lifecycle. Absence from a failed scan does not resolve an issue. Closure and false-positive decisions need a successful rescan and an explicit triage record. Reports are never automatically deleted or uploaded.

| Priority | Meaning |
| --- | --- |
| review | Code-quality finding requiring inspection |
| security-review | gosec pattern warning, **not a confirmed vulnerability or CVSS severity** |
| dependency-review | Vulnerability reported at module/package level, without proven symbol reachability in that finding |
| reachable-vulnerability | govulncheck reports a vulnerable symbol reachable in the analyzed configuration |

All findings currently produce a nonzero result, including dependency-review findings. This conservative default is a development gate, not an assertion that every warning is exploitable. Examine the raw report and code before changing behavior. Narrow suppressions need a rule-specific justification and a recorded decision; do not silently accept a new baseline.

## Incomplete scans and limits

Missing tools, wrong pinned versions, non-finding tool failures, malformed output, persistence errors, or changed inputs make the scan incomplete. Vulnerability JSON is parsed even when the tool exits zero; exit status alone cannot establish a clean vulnerability scan. An unreachable database is an error. Source changes during a scan require another scan; watch mode picks up the changed hash on its next iteration.

The watcher hashes Go files, go.mod/go.sum, `.golangci.yml`, and `scripts/devcheck.sh`, excluding hidden directories, vendor, and diagrams. It is not a filesystem snapshot or security sandbox. A→B→A changes between observations can be missed. Build tags, environment changes, tool replacement, and sources outside this scope require an explicit new scan. Vulnerability results depend on the selected toolchain/platform/build configuration and database at scan time.

A naru receipt only describes its last scan. `naru status` does not refresh vulnerability data. Always execute a new scan before completion/commit; do not infer current security from a cached passing receipt. Continuous inspection does not replace tests, review, Linux/Windows runtime checks, or clean-checkout CI.

References: [golangci-lint installation](https://golangci-lint.run/docs/welcome/install/local/), [CLI output options](https://golangci-lint.run/docs/configuration/cli/), [Go vulnerability management](https://go.dev/doc/security/vuln/).

## NARU-028 triage decisions

The initial 129 findings comprised 73 unchecked-error reports and 56 security review reports. They were reviewed by operation, not accepted as a baseline. No linter or required check was disabled.

- Persistence errors: Stop now blocks on failed NOT VERIFIED writes/removals. Receipt replacement cleans up temporary files and joins cleanup errors. Logs check header/footer writes, command-copy errors, sync and close before publishing a receipt. Regression tests first reproduced unsafe allowance, leaked temporary files, and four log failure paths.
- Output errors: CLI writers retain errors until the exit-code boundary. Successful commands cannot return zero after failed output. Gate allowance notices propagate errors; already-blocking diagnostic paths retain their blocking exit code. Hash writers and string builders cannot return write errors. Explicitly discarded formatting returns are limited to these cases.
- Permissions: newly created private state/log/receipt directories and files use 0700/0600. Test fixtures use private modes unless executable-bit behavior is under test. Existing file modes are not migrated. Public scaffold configuration and executable Git hooks intentionally retain their shareable modes.
- G204: each annotated command uses fixed executable/argv sources, except the intentionally trusted configured check executed through `sh -c`. NARU is not a sandbox for untrusted project commands. Test subprocesses use controlled fixture paths and arguments.
- G304/G703: annotated reads/writes use selected local project files, validated receipt names, generated log paths, Git-reported hook paths, or test-owned fixtures. These APIs do not claim confinement against a hostile local process. Hook installation still protects existing and special files.
- G122: development-source hashing is change detection, not a filesystem snapshot. Concurrent symlink changes are outside this boundary; adopting a newer root-scoped API would also exceed the current Go minimum. This residual limitation is explicit rather than represented as fixed.

Security review decisions are attached to individual operations with rule-specific `#nosec` comments. They do not exclude files or future findings. Review them when trust boundaries change. The initial report remains a historical snapshot; a fresh complete scan establishes the current finding count. NARU-005 now activates strict execution records, locking, cancellation history, and before/after verification. Its remaining recovery and platform limits are described in [execution records](execution-records.md).

NARU-005 additions were rescanned as they were introduced. G702/G703 reports on the subprocess test helper and explicitly selected local repository root were reviewed at their individual call sites. G304 reports on new test log/history paths were likewise restricted to test-owned fixtures. No global exclusions were added.
