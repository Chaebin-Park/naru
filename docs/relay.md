# Human-relayed checks

A relay lets a person run a configured check in an environment NARU cannot access and submit its result. NARU validates the request, current local inputs, reported execution and artifact bytes. It does not connect to the target or execute a remote command.

A valid relay result can satisfy that check locally, including Stop and pre-push. Other checks and the existing NOT VERIFIED condition still apply. CI must run `naru check --evidence ci` and use that command's result; `status` and pre-push are not substitutes for CI execution.

## Request and submission

```sh
naru relay new --target 'test workstation' --artifact execution.log --json example-check > request.json
# A person transfers the matching inputs and executes the requested check.
# Record the result and required artifacts in submission.json.
naru relay submit --file submission.json --json PACKET_ID
naru status --json
```

Issuing a request **replaces the current evidence for that check with pending evidence**. An earlier pass cannot satisfy the pending request. A new request or a local check supersedes it. Requests default to 24 hours; `--expires-in` accepts a positive duration up to 168 hours. Accepted evidence has the same expiry. Expiry is evidence freshness, not an exemption from checks.

`new --json` includes the packet, its hash and its local path. The packet describes the check command, work directory, timeout, declared requirements, input selection, input digest and expected artifact names. The person must run against those exact inputs. The submission identifies the packet and hash, reports start/finish times, before/after input digests, result/exit code, submitter and tool versions, and supplies the named artifact files with SHA-256 hashes.

The [submission example](../examples/relay/README.md) shows the wire fields. Its placeholders are not valid evidence. Replace them with the actual request identity, input observations, execution results and artifact hashes. NARU does not run the remote check or prove that the target used the claimed inputs.

Both reported input digests must match the request. Execution times must follow issuance, finish no later than local acceptance and precede expiry. `pass` requires exit 0; a valid failure is stored as failure and consumes the request. Do not resubmit a changed result under the same request: issue a new one. A new local run always produces its own result and cannot fall back to an earlier relay pass.

## Artifacts and failure handling

Supply every requested artifact exactly once, without extras. Artifact paths are relative to the submission JSON directory. Absolute paths, parent traversal, symlinks and special files are rejected. Each artifact is limited to 16 MiB, the combined total to 64 MiB, and the request to 16 artifacts. Request/submission JSON is limited to 1 MiB and rejects malformed, duplicate, unknown or missing fields.

NARU reads and hashes the actual artifact bytes, copies them into private local storage, rechecks current inputs and the definition, then publishes accepted evidence. Status rechecks stored artifact paths and hashes. Input changes, changed definitions, expired requests, missing/altered artifacts and replayed or superseded submissions cannot pass. Duplicate execution locks serialize request publication, submission and local checks.

A failure before authority publication leaves the pending request unsatisfied. A storage error after atomic publication may leave accepted evidence: inspect status before retrying. Unreferenced exports or artifact copies left by interrupted attempts are not evidence. The application does not overwrite accepted artifacts; the filesystem owner can still edit them, which makes their stored hashes fail validation.

## Trust, privacy and compatibility

The provenance is `human-relayed`. This is a human assertion with consistency checks, not authenticated remote execution, proof of remote file contents or verification of the person's identity. Tools, versions and remote timestamps are reported facts. Local clock manipulation and hostile same-user filesystem races are outside the guarantee. Evaluation rejects a local time before acceptance or at/after expiry.

Packets and artifacts stay under ignored `.naru/relay/` storage. NARU does not collect environment variable values or print artifact contents. A configured command or a supplied log can itself contain secrets: inspect them before manual transfer or sharing. NARU preserves exact command identity and does not silently redact it.

Relay evidence uses schema 3 and the `human-relay-v1` contract. Existing schema 2 local execution semantics and schema 1 compatibility remain unchanged. Old binaries that do not understand relay evidence must not be used to interpret it as a pass. Relay and local executions share one current authority per check; history preserves their earlier records separately.

This version supports configured checks only. Surface/baseline aggregation, remote execution, automatic transfer and actual CI environment setup are separate work. Submitting a relay does not clear NOT VERIFIED; the existing verified Stop recovery must clear that marker before pre-push can allow completion.

Exit codes: 0 request created or passing submission accepted; 1 stale, expired, non-current or unusable evidence, or a recorded nonpassing result; 64 invalid arguments/config/protocol or an unreadable submission file; 70 authority-storage/publication or internal failure; 130 interruption.

An unreadable request packet or source artifact is unusable evidence (exit 1). Failure to read the supplied submission JSON is invalid input (exit 64). Authority-state I/O and publication failures are internal/storage errors (exit 70).

## Verification scope

Automated acceptance covers request issuance, strict submission validation, replay/expiry/input changes, artifact corruption, concurrent submission, local/CI replacement and Stop/pre-push decisions. Store fault injection covers archival followed by failed publication, retry, and durability failure after authority publication. These tests use synthetic local projects; they do not prove execution on a remote machine.

The task gate records the implementation revision and full verification result in `docs/tasks/NARU-014.json`. Go 1.27.3 on macOS was used during implementation. Minimum Go 1.23 and Windows/Linux runtime behavior were not tested; cross-platform vet is separate evidence. Actual Git push tests were not enabled.
