# Common completion report

Status, Stop and pre-push evaluate the same selected requirements through `status.Evaluate`. Evaluation captures one instant for relay validity; `status.EvaluateAt` supports deterministic comparisons against current files at a supplied instant. It is not historical replay or an atomic snapshot.

## Selecting requirements

Existing `modes` still select checks, and every mode must contain at least one check. Additional requirements are explicit:

```yaml
modes:
  code: [test]
  docs: [fmt]
mode_surfaces:
  code: [api]
mode_policy:
  code: true
```

All referenced modes, checks and surfaces must exist. Repeated surface names are rejected. Selecting a pack or declaring a surface does not automatically require approval. Policy findings and equality baselines are different authorities; requiring a policy does not require equality approval for its findings surface.

## Shared decision

| Evidence | Satisfied condition | Blocking examples |
| --- | --- | --- |
| Local check | Current terminal passing evidence | Missing, failed, stale, not-run, invalid, active or interrupted |
| Human relay | Accepted passing evidence, valid at evaluation time, matching current inputs/definition/artifacts | Pending, expired, replayed, changed or invalid evidence |
| Required surface | Current ready candidate matches a reviewed baseline | Missing candidate/baseline, unapproved legacy snapshot, changed, stale, busy or corrupt |
| Required policy | Compatible clean, known or resolved findings | Missing, bootstrap-required, new, stale, busy, corrupt or incomplete |

Known policy findings remain warnings. Resolved findings do not automatically shrink the approved baseline; human approval is required. Reintroduction after an approved reduction is new and blocks. Evaluation never runs extraction or performs approval.

## Adapter differences

| Adapter | Mode | Common evidence unsatisfied | Common evidence satisfied with NOT VERIFIED marker |
| --- | --- | --- | --- |
| `status` text/JSON/Markdown | Explicit flag, environment, stored mode, default | Exit 1; inspection failures return 70 | Exit 0 with separate warning/marker |
| Stop | Environment, stored mode, default | Blocks initially; documented D13 repetition escape may allow NOT VERIFIED | Clears marker only after verified evaluation and successful state cleanup |
| Pre-push | Default mode only | Exit 1; no D13 escape | Exit 1 until a verified Stop clears the marker |

`NARU_GATE=off` remains a Stop-only NOT VERIFIED escape. A narrower selected mode does not weaken pre-push. All behavior is local working-tree evaluation, not a guarantee about each Git ref or clean-checkout CI. Actual push and live agent integration are separate evidence.

## Output compatibility and privacy

Check-only reports retain JSON schema 2. Reports with selected surface or policy requirements use schema 3, preserving check rows and adding `surfaces` and/or `policy`. Surface rows contain state and input/candidate/baseline identifiers. Policy exposes counts and digests without finding messages or locations. Relay rows preserve provenance without exposing submitted artifacts.

`status --markdown` renders the same report and exit decision as JSON; the output options are mutually exclusive. Untrusted table cells are escaped. Surface and policy evidence participate in the Stop progress fingerprint. Locks protect individual resources; files can still change across resources, and A→B→A mutations remain outside before/after detection guarantees.

## Verification map

- `TestCommonReportIntegration`: combined relay, baseline and policy requirements; fixed-instant report equality; D13 marker and strict pre-push.
- `TestSurfaceDecisionIntegration`: missing, unapproved legacy, stale, changed, busy, approved and malformed surface evidence; narrowed mode versus pre-push.
- `TestPolicyDecisionIntegration`: missing, bootstrap, known, stale, resolved, approved reduction, reintroduced and malformed policy evidence.
- `TestRelayWorkflow`: other required checks and NOT VERIFIED cannot be bypassed by relay success.
- `TestReportEvaluationTime`: acceptance and expiry boundaries.
- `TestStatusMarkdown`, `TestMarkdownReport`: output decisions, escaping, privacy and writer errors.

These are automated fixtures, not new live agent-session or actual-push observations. Independent MVP review remains a separate task.
