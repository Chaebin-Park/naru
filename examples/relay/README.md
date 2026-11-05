# Relay submission example

This is a wire-format illustration, not a runnable passing receipt. The
placeholder identifiers, hashes and times in `submission.json` must be replaced
with a real request and observed execution data. Do not submit this example as
proof of a check.

1. Save `naru relay new --target 'test workstation' --artifact execution.log --json CHECK` output as `request.json`. Issuance replaces that check's previous current evidence with pending evidence.
2. Inspect the packet command and the files before transferring them yourself.
   No command or file is sent automatically. Run the check in the target
   environment against the requested inputs and collect its real result/log.
3. Build the submission from the returned packet identity/hash, check name,
   actual UTC start/finish times, submitter and reported tool versions. Supply
   before/after input digests for the checked files. Their hash, count and source
   must equal the request; they remain human claims, not remote authentication.
4. Place `execution.log` beside the submission and compute its SHA-256. Replace
   the example hash with `sha256:` followed by the lowercase 64-hex digest.
   NARU reads and hashes the bytes again when accepting and evaluating evidence.
5. Run `naru relay submit --file submission.json PACKET_ID`. A pass needs exit 0;
   a valid failure is recorded as failure and consumes the request. Repeating
   submission or using superseded/expired requests is rejected.

Do not include secrets in a public request or log. NARU does not automatically
redact commands or submitted artifacts. They remain ignored local files.

CI must rerun the actual check with `naru check --evidence ci`, regardless of
relay success. See [relay semantics and limits](../../docs/relay.md).
