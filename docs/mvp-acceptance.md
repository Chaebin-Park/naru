# MVP acceptance evidence

Updated 2026-11-06. **Required acceptance evidence is collected within the documented support boundaries.** After explicit maintainer approval, actual temporary-local-remote pre-push integration passed. Independent review found no remaining implementation blocker; its tool limitations and platform gaps remain explicit below. Final verification/closure is recorded in the NARU-022 task ledger; the subsequent v0.2.0 publication is recorded in NARU-035 and [release verification](release-preparation.md#published-v020-verification).

## Acceptance matrix

| Criterion | Requirements | Evidence | Status / limit |
| --- | --- | --- | --- |
| AC-01: unsafe or absent evidence cannot pass | FR-03/04/07; NFR-01/04 | `TestCorruptReceiptBlocksEvaluation`, `TestUnavailableRequirementsCLI`, runner cancellation/storage tests; [execution records](execution-records.md) | Automated checks; before/after hashing and process ownership limits remain explicit |
| AC-02: actual Git push/ref behavior | FR-06 | `TestPrePushLocalRemote` covers missing, passing, NOT VERIFIED, stale and narrowed mode with remote ref assertions | Passed after explicit approval on 2026-11-06; actual push exit and remote ref existence matched for all five states |
| AC-03: extract/diff/approval | FR-08/09 | `TestCandidateDiffLifecycle`, approval tests and recorded interactive evidence; [baselines](baselines.md), [verification](verification.md) | Existing automated/interactive evidence; approval is not identity authentication |
| AC-04: policy reduction | FR-14 | `TestPolicyReductionLifecycle`, `TestPolicyFixtures`, `TestPolicyDecisionIntegration`; [policy](policy.md) | Known/new/resolved/reintroduction and freshness covered |
| AC-05: human relay | FR-12 | `TestRelayWorkflow`, `TestRelayStatus`, `TestReportEvaluationTime`; [relay](relay.md) | Input/artifact/expiry/provenance boundaries; does not authenticate remote claims |
| AC-06: three public platform samples | FR-10/11/15/16; NFR-09 | Spring/Android/Web fixture tests and fresh public smoke below; [packs](packs.md), [Android](android-verification.md), [Web](web-verification.md) | Bounded public samples, not all frameworks or production workloads |
| AC-07: common decisions and adapter exceptions | FR-05/17/18 | `TestCommonReportIntegration`, `TestAgentHookPayloads`, `TestCodexPatchGuard`, report tests; [common report](common-report.md) | Shared conditions agree; D13 and pre-push strictness documented; prior live sessions in verification |
| AC-08: real CI failure/recovery | FR-18 | [Actual failed and corrected GitHub runs](ci-smoke.md) | Completed on exact revisions, Ubuntu/Go 1.23.12, exit 1 then 0; no artifact upload |
| AC-09: installation, runtime limits and measurement | FR-19; NFR-03/06/07/10 | [Candidate smoke and checksums](release-preparation.md), [three-check measurement](reliability-performance.md) | Native install smoke and 3-check/30-sample p95 1.109s; runtime gaps below |
| AC-10: traceability and independent review | NFR-12 | This matrix, task records and independent review below | Evidence mapped; independent verdicts and limitations below |

## Remaining requirement traceability

FR-01 initialization and FR-02 mode selection are covered by `TestInitDetectsStackAndRefusesOverwrite`, `TestInitWithoutStackFailsClosed`, `TestModePrecedenceAtCLI` and the candidate smoke. FR-13 (time-limited waivers) is explicitly deferred beyond MVP; it is not implemented or counted as accepted.

| Nonfunctional requirement | Evidence / boundary |
| --- | --- |
| NFR-01 accuracy | AC-01/03/04/05/07; uncertainty blocks rather than creating passing evidence |
| NFR-02 determinism | Surface canonicalization fixtures and injected common report time; stable output excludes volatile build data |
| NFR-03 latency/resources | Three-check workload, 30 measured samples, median/p95/RSS; first status is not cold cache |
| NFR-04 interruption/storage | Cross-process ownership/cancellation and storage failure regressions; physical power loss not tested |
| NFR-05 safe execution | Gate evaluation does not execute configured checks; interactive approval and relay boundary tests |
| NFR-06 confidentiality | Sample allowlists, synthetic fixtures, no config values in surfaces; candidate byte/path and public diff inspection; logs remain local sensitive material |
| NFR-07 compatibility | Native macOS suite, bounded Linux and Go 1.23.12 CI, cross-build targets; Windows runtime/full minimum-Go suite not verified |
| NFR-08 explanation | Text/JSON/Markdown reports, reasons/provenance and writer-failure tests; report schemas documented |
| NFR-09 extensibility | Three repository-owned packs and public runtime scenarios; no new core branch introduced by this acceptance pass |
| NFR-10 reproducibility/offline | Source-only CI archive, local status/diff, cached/offline Web preparation; building other samples can require cached dependencies |
| NFR-11 usage control | No model call in gate evaluation; D13 repetition and NOT VERIFIED behavior retain their tests |
| NFR-12 traceability | FR/AC/NFR mappings here and committed `docs/tasks` ledger; private planning is not shipped |

## Fresh public sample execution

On 2026-11-06 a new temporary project was populated from committed `packs` and `examples` at `bda91eaea0274a4d0c87c41fe9c8fd5ac6d5b078`. No existing receipts were copied. The source-equivalent `089cbd3` macOS candidate ran separate modes:

| Mode / check | Tools selected | Result |
| --- | --- | --- |
| spring / `spring-boot.contracts` | Installed JDK 21.0.9 and Maven 3.9.16 | PASS, 7 inputs, 3.51 seconds |
| android / `android.contracts` | Installed JDK 17.0.19, Gradle 8.11.1, AGP 8.9.2, SDK 35/build tools 35.0.0 | PASS, 10 inputs, 8.35 seconds |
| web / `web.contracts` | Node 22.22.2, npm 10.9.7 | PASS, 9 inputs, 0.87 seconds |

No SDK/system installation, emulator or production service was used. These are fresh build/extraction successes, not new human approvals or new live coding-agent sessions. Earlier mutation/approval scenarios remain in their platform verification documents.

## Independent review and correction

Separate read-only Codex CLI sessions loaded the installed code-reviewer and architect role instructions and reviewed `534639f..4a6fc3c` independently. The code lane returned REQUEST CHANGES (one medium finding, no critical/high findings); architecture returned WATCH. Both identified candidate source provenance: a clean Git status does not exclude ignored source, and ambient Go overlays can substitute source while labeling binaries with HEAD.

The author reproduced ignored-source injection, fixed the build to use a single committed source export with Go workspace/overlay settings disabled, and passed all three initial regression scenarios in `scripts/test_prepare_candidate.py`. The fix is `0a6fdae`. Follow-up code review closed the original medium defect with COMMENT and zero new defects; architecture returned CLEAR/COMMENT with a low-priority archive-attribute concern. An author regression then proved that ambient export-ignore could omit a committed Go initializer while the binary still built. The follow-up fix reads exact Git tree/blob objects instead of attribute-sensitive archives and adds attribute/valid-overlay regressions. Final independent follow-up at `60dab96` found no remaining implementation blockers: code verdict COMMENT with zero findings, architecture CLEAR. Six author-run regressions passed; reviewers did not rerun builds/tests. LSP/AST-grep were unavailable in the review environment, so no unconditional review approval is claimed. The local-push acceptance gap was subsequently closed by the approved execution below. No evaluator false-pass defect was reported in these bounded reviews, which is not a guarantee that none exists.

## Deferred work and support limits

- Windows runtime/process-tree behavior, full Go 1.23 suite, full native Linux suite and cold-cache performance: explicitly unverified environments/scenarios; cross-vet/build is not execution evidence.
- Complete filesystem snapshot isolation, A→B→A detection and escaped process descendants: documented lifecycle boundaries; stronger guarantees require separately scoped design.
- Time-limited waivers, additional framework adapters, multiple CI-provider templates and PR posting: post-MVP backlog.
- Binary signing and repository settings remain separate maintainer decisions. The approved v0.2.0 tag and release were published under NARU-035; candidate preparation alone never publishes.

## Approved actual local push execution

On 2026-11-06 at approximately 17:05 KST, after explicit maintainer approval, source `c3ba4dd5ea2f2c7a9468aad194ec3996d469b9c7` ran:

```sh
NARU_TEST_LOCAL_PUSH=1 go test ./internal/cli -run '^TestPrePushLocalRemote$' -count=1 -v
```

`TestPrePushLocalRemote` passed (1.61 seconds; package exit 0). The test creates separate temporary working/bare repositories, builds the CLI, installs the real executable pre-push hook only in that fixture, performs actual Git pushes, and checks each destination ref. Missing evidence, NOT VERIFIED, stale input and a narrowed mode were rejected without ref creation; fresh passing evidence permitted the push and created its ref. No GitHub branch, main ref, user repository hook or system installation was changed by this test. This validates the documented working-tree/default-mode gate, not every commit/ref included in arbitrary pushes.
