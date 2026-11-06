# Policy index and known violations

Policy links existing tool rules to their configuration, rationale and executable fixtures. It does not define a new lint language. A project adapter returns complete findings through one configured surface. Existing violations warn, new violations block, and resolved violations remain in the approved known set until a person approves a reduction.

Contract baselines and policy baselines have separate authority. `baseline approve` approves an external contract; it cannot approve new policy violations. Policy commands do not implicitly change Stop/pre-push requirements. Use `mode_policy` to explicitly require policy evidence in the common gate report.

## Index

Create `.naru/policy.yml` and reference an existing surface from `.naru/config.yaml`:

```yaml
version: 1
surface: policy-findings
definition_files: [tools/policy-adapter.py]
sources:
  DEC-001: docs/decisions/no-debug-api.md
rules:
  no-debug-api:
    tool: api-check
    tool_config: tools/api-rules.json
    source: DEC-001
    fixtures:
      - project: testdata/policy/no-debug-api/pass
        expect: pass
      - project: testdata/policy/no-debug-api/fail
        expect: violation
```

The adapter, tool configuration, rationale file and fixture projects must exist. Each rule needs pass and violation fixtures. Paths are repository-relative and may not contain symlinks or escape the repository. Unknown fields, duplicate keys, null values, invalid rule/source references and missing files are rejected. Declare adapter code and other behavior-defining files in `definition_files`; undeclared external tool behavior is not authenticated by NARU.

Version 1 supports one index and one findings surface per repository. Bounds: 1 MiB index, 128 rules, 256 referenced definition/source files, 256 fixtures, 1 MiB per definition/source file and 16 MiB combined. Each copied fixture permits 1,024 regular files and 16 MiB total. The existing surface output limit is 1 MiB.

## Adapter output

Use the existing `{schema: 1, items: [...]}` surface output. Every registered rule must emit exactly one coverage item, even if it found no violations:

```json
{"id":"coverage:no-debug-api","value":{"kind":"coverage","rule_id":"no-debug-api","complete":true}}
```

A finding value has `kind: "finding"`, `rule_id`, `stable_location`, `fingerprint`, `file`, `line`, and `message`. Its outer item ID is `finding:` plus the lowercase SHA-256 of the JSON array `[rule_id, stable_location, fingerprint]`. The fingerprint is a lowercase 64-hex digest supplied by the adapter. Stable location identifies the affected construct; display line/message are not identity. Match Go's JSON array encoding when computing the ID.

Missing/false coverage, unknown rules, duplicate identities, malformed values and extraction errors reject the entire result. Zero violations means coverage items with no findings, not missing output. Completeness remains a trusted local adapter assertion, not proof against a dishonest adapter.

## Commands and review

```sh
naru policy list --json
naru policy test
naru policy check --json
naru policy bootstrap
# After fixing existing violations:
naru policy check
naru policy approve
```

`list` validates the index and references without running a scan. `test [rule-id]` runs fixture copies in temporary directories. The selected surface definition, exclusions, adapter and tool configuration must match the parent project. Errors or missing environments do not count as passing fixtures. Source fixtures and policy baselines are not modified.

`check` refreshes a candidate and reports sorted `known`, `new` and `resolved` findings. Without a baseline it reports `bootstrap-required` and does not pass. Known findings warn; new findings block. A reduction candidate does not change the baseline.

`bootstrap` explicitly accepts the initial known set only when the policy baseline is absent. `approve` accepts a reduction only: it cannot add new findings or migrate changed policy definitions. Both require terminal stdin and stderr, a self-reported reviewer, a reason and confirmation of the exact candidate digest. Empty targets require an explicit empty confirmation. Review is not identity authentication.

Before publication, the policy and surface locks protect revalidation of the candidate, current source surface state, current inputs, definition and prior baseline. A changed preview, failed intervening extraction, new finding or cancelled review preserves the baseline before publication. Output/cancellation errors after atomic publication may leave the new approved file; inspect it before retrying.

## Storage, freshness and limits

- `.naru/state/policy/candidate.json`: current running/failed/ready candidate.
- `.naru/policy-baseline.json`: approved known findings and review metadata, published as one file.
- `.naru/state/locks/policy/state.lock`: dedicated policy lock, independent of user check names.
- Existing surface state: used as extraction authority, never as policy approval.

Definition digests bind the index, selected surface definition and exclusions, declared adapter/tool configuration, and rationale files. Changing rule scope or definitions makes an existing baseline incompatible; it must not appear to resolve violations. Version 1 has no automatic migration, reset or growth-approval command.

Source content has a separate input digest, so ordinary code fixes remain comparable. Candidates and review previews must match current inputs, but old baseline source hashes are not required to match current source. After a reduction is approved, a removed identity reappears as new.

Local files remain editable by their owner. Deleting the baseline is a manual policy reset, not an automatic repair supported by these commands. Detached/malicious adapters and same-user concurrent file tampering are outside the process and locking boundary. No tools are installed and no remote processes are started.

Exit codes: 0 successful list/fixtures/review or compatible check without new findings; 1 new findings, missing baseline, unavailable/stale candidate, incompatible definitions or failed fixtures; 64 usage/index errors; 70 storage/internal errors; 130 interruption. Runtime platform support follows the existing extractor and terminal-review limits.

## Verification evidence

NARU-012 acceptance covers strict index references, complete findings, a 10-to-7 reduction with reintroduction, stale review rejection, failed/cancelled publication, actual adapter fixtures and CLI exits. The ordinary check name `policy` lock collision was reproduced before introducing a separate policy lock namespace.

On 2026-11-05, Go 1.27.3 on macOS arm64 passed format, native vet, the full race suite, Windows/Linux cross-vet, build and the built binary's four self-checks. Development analysis completed with zero findings. Six real terminal-review scenarios covered rejection, interruption, exact bootstrap, input mutation during review, empty reduction and reintroduced findings. These are terminal CLI tests, not live AI-agent connection evidence. Go 1.23 and other OS runtimes were not executed; opt-in push integration was not run.

The first isolated task verification caught an unrelated lifecycle fixture race: a shell polling loop could leave a child during cancellation, correctly producing `owner_lost` rather than the test's expected clean cancellation. The fixture now uses a single exec'd helper that announces readiness after exec. Cancellation, history preservation, gate rejection and successful clean restart assertions remain required; uncertain cleanup is still covered separately.

## Required policy evidence

Opt in with `mode_policy: {code: true}` for an existing check mode. The common status report, Stop and pre-push inspect current policy evidence without executing adapters or approving changes. Missing, stale, busy, invalid, bootstrap-required or new findings block. Compatible clean, known and resolved results follow `policy check` semantics: known findings warn, and resolved findings still require explicit human approval before shrinking the stored baseline. Reintroduction after approved reduction is new and blocks.

Extended reports use schema 3 and expose only policy state, finding counts and evidence digests, not finding messages or source locations. Existing check-only reports remain schema 2. Stop retains its documented selected-mode and NOT VERIFIED behavior; pre-push enforces the default mode and rejects NOT VERIFIED markers. Locks are per resource, not a global filesystem snapshot. Policy enforcement does not imply surface equality approval for the findings surface; configure those requirements independently only when both meanings are intended.
