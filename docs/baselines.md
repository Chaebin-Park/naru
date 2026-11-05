# Surface candidates, diffs and approval

`naru baseline extract <surface>` runs a declared surface extractor and saves a local candidate. `naru baseline diff <surface>` compares a fresh candidate with a read-only baseline. Flags such as `--json` precede the surface name. Neither command approves or replaces a baseline.

```sh
naru baseline extract --json dummy.contract
naru baseline diff --json dummy.contract
```

Declare a surface directly in config or through a [local pack](packs.md). It uses `run`, `inputs`, `workdir`, `timeout`, `requires`, and `extractor_version`. Stdout follows the [surface JSON contract](surface-contract.md); stderr is diagnostic text. This supports trusted project extractors, not automatic language/AST discovery.

## Execution and candidate state

The extractor receives the schema 1 request on stdin and `NARU_SURFACE`/`NARU_ROOT` environment variables. Commands run with `sh -c`. The internal fileset hash's `sha256:` prefix is removed at the request boundary; surface input digests remain 64 lowercase hex characters. The same conversion is reversed when checking freshness.

The command acquires the surface lock, reloads config, and publishes `running` at `.naru/state/surfaces/<surface>.json` before inspecting prerequisites or starting a process. A successful candidate requires:

- available declared OS/tools and implicit `sh`, a root-contained workdir without symlinks, and nonempty observed inputs;
- successful process completion, bounded stdout (1 MiB), and valid extractor JSON;
- reloaded definitions and inputs matching their before-execution digest and file count;
- a checked candidate write, sync, close and atomic replacement.

Only then does state become `ready` with a snapshot. Unavailable tools, invalid output, cancellation, timeout, execution failure or changed/unreadable inputs produce `failed` with a reason and no snapshot. Previous ready authority is replaced before execution; it cannot reappear after failure. Storage failure returns an error. If final publication fails, the earlier `running` state cannot satisfy diff. Candidate state is current authority, not an immutable execution-history archive.

Capture uses a bounded writer that does not expose bypassing `io.ReaderFrom` methods. Excess output cancels the process; stderr retains a bounded tail for diagnostics. On Unix, cancellation kills the shell process group. Detached processes are not sandboxed. A leftover running record or a child holding inherited output open after the wait bound requires manual recovery; NARU does not infer child termination from a free lock. Preserve the state, inspect/stop surviving children, then move the affected candidate file before retrying. Do not delete permanent lock files. Windows cancellation/process-tree runtime remains unverified and can require recovery.

Candidate files use mode 0600 and directories 0700. Symlinks and special entries in evidence paths are rejected. Snapshot identities, duplicate keys, canonical bytes and content digest are checked when read. These are local integrity checks, not signatures or authorization. There is no filesystem snapshot or physical power-loss durability guarantee, and no defense against a hostile same-user process racing paths. Locks serialize cooperating surface operations; a same-named ordinary check lock may conservatively contend with a `surface-`-prefixed surface lock.

## Diff and empty results

An approval record is read from `.naru/baselines/<surface>.json`. It contains the snapshot and review metadata in one file. Ordinary extraction never touches this path. Old bare snapshots remain readable for comparison but require explicit approval before they can produce `unchanged`. A record does not authenticate who approved it.

Diff first requires a ready candidate whose input digest matches current files and definitions. Missing, failed, running, recovery-required or stale candidates cannot produce an unchanged report. Corrupt state is an error, not a missing-baseline fallback.

The report contains `surface_id`, `state`, `old_digest` (null if absent), `new_digest`, `input_digest`, `candidate_count`, `baseline_revision`, `definition_changed`, and sorted `added`, `removed`, `modified` arrays. Added/removed items include values; modified items include before/after values. The states are:

| State | Meaning |
| --- | --- |
| `missing-baseline` | First candidate needs approval, including an explicitly empty first result |
| `changed` | Content or extractor definition changed; review is required |
| `approval-required` | A legacy bare snapshot has no review metadata |
| `unchanged` | Content digest matches the existing baseline and the candidate is current |

An explicit successful empty list can produce removals, but is never silently accepted as a deletion. No stdout, extraction errors and malformed JSON cannot produce an empty candidate. Version changes can produce `changed` even when item values match, indicated by `definition_changed`. Different input digests with the same observed content are expected; freshness and contract content remain separate.

## Interactive approval

```sh
naru baseline approve --json dummy.contract
```

Approval requires real terminal stdin and stderr on macOS or Linux; stdout may be redirected. Other OSes reject approval until terminal support is implemented. There is no `--yes` or noninteractive bypass. The command prints the full diff and input/content digests to stderr, asks for a self-reported reviewer and reason, and requires `approve <full-new-digest>`. An empty candidate requires `approve empty <full-new-digest>` so removing every item is explicit.

Immediately before publication, NARU reacquires the surface lock, reloads config/candidate/baseline, and recomputes input freshness. It compares the candidate content digest, input digest, and exact baseline-file revision with the preview. Any change rejects approval; rerun extraction for stale inputs and review a fresh diff. This recheck does not rerun the extractor. Concurrent external file mutation is not isolated by a filesystem snapshot.

The schema 1 record contains `snapshot` and `approval`. Approval includes a random `id`, `old_digest` (null for the first baseline), `new_digest`, `input_digest`, `reviewer`, `reason`, and UTC `approved_at`. Unknown/duplicate fields, invalid metadata and inconsistent digests are rejected. The record is written, synced and closed as one temporary file, then atomically renamed. Failure or interruption before publication preserves the prior baseline; no metadata sidecar can become half-published. An interruption or output error after publication may still leave an approved record: inspect the file or rerun diff before retrying. Physical power-loss durability is not guaranteed.

Terminal interaction is a review workflow, not proof of human identity. Another process with the same user's access can drive a terminal or edit local records. NARU does not claim approval authentication, hostile-process isolation, or automatic integration of surface results into existing check modes.

## Exit codes and current boundary

- Extract: 0 ready candidate, 1 unavailable/failed/invalid extraction, duplicate or recovery required, 64 usage/config/empty inputs, 70 storage/internal error, 130 caller interruption.
- Approve: 0 published, 1 cancelled confirmation/stale or unavailable candidate/busy lock, 64 nonterminal or usage/config, 70 invalid review/storage/internal error, 130 interruption.
- Diff: 0 unchanged, 1 missing baseline/change/approval or unavailable/stale candidate, 64 usage/config, 70 corrupt evidence/IO/internal error. Unavailable candidate diagnostics go to stderr; successful comparisons can use JSON stdout.

A candidate alone does not satisfy existing check modes or hooks. There is no implicit surface-to-gate policy, automatic approval, relay, or remote execution. Minimum Go and Windows/Linux runtime evidence remain pending; cross-platform vet alone is not runtime testing.

Evidence: `TestSurfaceExtractionLifecycle`, `TestSurfaceCancellationAndRecovery`, `TestSurfaceCaptureLimit`, `TestSurfaceWorkdirRejectsSymlink`, `TestCandidateDiffLifecycle`, `TestCandidatePublicationFailure`, `TestBaselineCLI`, and `TestSnapshotValidation`.

Approval evidence: `TestApprovalLifecycle`, `TestApprovalPreviewChanges`, `TestApprovalPublicationFailure`, `TestApprovalMalformedRecords`, `TestApprovalRequiresTTY`, and `TestApprovalReviewFlow`.

## Unresolved extractor cleanup

After a Unix extractor exits, a bounded read-only process-group check detects remaining members even when an exit error hides the output wait limit. Remaining members or inspection errors publish `recovery/owner_lost`, never a reusable candidate. Caller interruption still returns exit 130. A subsequent extraction is refused until manual recovery. The runner does not repeatedly kill a numeric group ID after wait; the ID could belong to another group. Deliberately detached processes remain outside the process-group boundary.
