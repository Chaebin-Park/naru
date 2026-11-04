# Surface extraction contract

NARU-007 defines the internal protocol and canonicalization used by later pack and baseline work. `internal/surface` validates output and builds a snapshot in memory. It does not run a process, read or write a baseline, approve changes, or add a CLI command. Configuration remains version 1; execution records remain schema 2. This protocol has its own schema 1.

A surface is a named set of observable contract items, such as routes or configuration keys. The extractor owns domain meaning; the common layer preserves it and rejects ambiguous output.

## Request and process boundary

`RequestBytes` validates and encodes one UTF-8 JSON object for extractor stdin:

```json
{"schema":1,"surface_id":"server/routes","extractor_version":"routes-v1","input_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
```

The digest above is illustrative, not verification evidence. All four fields are required. IDs and extractor versions match `[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}`; they are identifiers, never filesystem paths. `input_digest` is a lowercase 64-character SHA-256 hex string. The caller supplies it from observed inputs and relevant definitions, including pack/extractor identity and normalization policy. The extractor cannot override it in stdout. An extractor must change its version when its filtering, interpretation, or output semantics change. Declared versions are not authenticated binary identities.

The future execution adapter sends exactly this request on stdin, collects stdout separately from diagnostic stderr, and passes **every** process/cancellation/capture/size error to `Build`. Only successful completion supplies a nil error. A valid-looking stdout from a failed process is rejected. The adapter must enforce the output limit while capturing; the in-memory builder cannot limit an already allocated buffer. Timeouts, workdir, environment requirements, locking, and before/after input verification must be provided by that adapter before a snapshot becomes evidence. NARU-007 does not claim to implement those lifecycle guarantees.

## Extractor output

Successful stdout contains exactly one JSON object, with no progress messages:

```json
{
  "schema": 1,
  "items": [
    {"id": "route/users", "value": {"method": "GET", "path": "/users", "responses": [200, 404]}},
    {"id": "config/limit", "value": {"default": 100, "required": false}}
  ]
}
```

The only envelope keys are `schema` and `items`. Each item contains only `id` and `value`. Item IDs use the identifier syntax above and must be unique within the surface. Values must be objects; their nested keys and JSON values are domain-specific. An explicit `{"schema":1,"items":[]}` means a successfully observed empty surface. Missing/blank stdout, null/missing items, errors, or malformed JSON never mean an empty surface. A valid empty surface is **not** permission to delete an approved baseline; later diff/approval policy handles that change.

The builder rejects unknown envelope/item fields, unsupported schema (including non-integer spellings such as `1.0`), duplicate keys at any depth (including escaped aliases), duplicate item IDs, invalid UTF-8, isolated Unicode surrogate escapes, trailing values, output over 1 MiB, and JSON values nested deeper than 64 from the root at depth zero. It returns no partial snapshot on failure. Nested `value` objects are intentionally extensible; their keys are not silently discarded.

## Canonical bytes and digest

The policy is `surface-json-v1`, not RFC 8785. Its exact serialization contract is:

1. Emit a compact JSON object in field order `schema`, `policy`, `surface_id`, `extractor_version`, `items`, without a trailing newline.
2. Sort items by ID using UTF-8 byte order. Emit each item's keys in order `id`, `value`.
3. Sort all keys inside value objects using UTF-8 byte order. Preserve every field, array element and array order. Empty objects, empty arrays, null, booleans, strings and numbers remain distinct.
4. Preserve JSON number spelling exactly using `json.Number`, without floating-point conversion. Large integers stay exact. `1`, `1.0`, `1e0`, `0` and `-0` are deliberately distinct; extractors that consider them equivalent must emit a stable domain representation.
5. Normalize JSON string escaping using Go's `encoding/json.Marshal`: compact UTF-8, standard JSON escapes, HTML-sensitive characters and U+2028/U+2029 escaped. Equivalent escape spellings become the same decoded string. Unicode normalization, case folding, path rewriting, and whitespace trimming **inside strings** are not performed.
6. SHA-256 the resulting bytes and encode the digest in lowercase hex. Schema, policy, surface ID and extractor version are part of those bytes.

The fixture [canonical routes](../internal/surface/testdata/routes.canonical.json) shows the exact document (its file newline is excluded from hashing). A fixed hash in `TestCanonicalFixtures` locks the byte policy. A policy change requires a new policy identifier and explicit baseline compatibility handling, not silent reuse.

`Snapshot` contains schema, surface/extractor identities, `input_digest`, policy, canonical bytes and the content `digest`. The content digest excludes `input_digest`: two different source inputs can produce an unchanged observable contract. Freshness and approval must still compare their own exact input/candidate identities. Matching content digests alone do not prove that the current source was extracted, that execution succeeded, or that the result was approved. Snapshot fields are not authenticated, and this package does not implement persisted-snapshot decoding.

## Volatile fields and semantic preservation

There is no generic ignored-field list. A field called `timestamp`, `path`, `version`, or `description` can be a real contract requirement. Common code must not delete it by name. Each extractor documents which non-contract data it omits (for example a generator's execution timestamp), emits only safe public contract values, and versions that policy. Fixtures must demonstrate both stable output after those omissions and a digest change for meaningful changes. Domain-specific filtering and full language AST extraction are later pack work; production credentials/configuration values must not be copied into fixtures.

## Verification and limits

- `TestCanonicalFixtures`: reordered keys/items, whitespace and equivalent escapes produce fixed canonical bytes and digest across repeated normalization.
- `TestSemanticChanges`: method, item membership/identity, array order, exact numbers, types and extra fields affect the digest; input freshness remains separate; extractor/surface identity changes affect it.
- `TestInvalidExtraction`: failed execution with valid stdout, missing output, explicit empty result, malformed/ambiguous JSON, Unicode, size and depth boundaries stay distinct.
- `TestFixturesAreCheckInputs`: repository test receipts include testdata, so fixture edits invalidate earlier success.
- `TestRequestContract` and `FuzzBuild`: request validation, no partial result on failure, and deterministic accepted output.

These tests replay synthetic output; they do not claim that a real extractor is deterministic. NARU-008 provides repository-local pack declarations/loading. NARU-009 provides extraction execution, candidate storage and diff; NARU-010 provides approval. No external downloads, dependencies, tool installation or existing check/gate behavior changes are introduced here.
