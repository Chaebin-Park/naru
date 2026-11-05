# Web contract pack

This pack builds and tests the public Node HTTP example in `examples/web`.
It supports one static registry and one local workspace package, using Node
22.22.2, npm 10.9.7, npm lockfileVersion 3 and Python 3.9+ on macOS/Linux.
It is not a framework scanner or a general npm dependency resolver.

The pack adds `web.routes`, `web.dependencies`, `web.env` and `web.contracts`.
Every mode performs the same fresh preparation, runtime tests and export
validation. Contracts validates the build without approving or comparing
baselines. The other modes print one deterministic schema-1 surface document.

```sh
python3 -B packs/web/extract.py routes
python3 -B packs/web/extract.py dependencies
python3 -B packs/web/extract.py env
python3 -B packs/web/extract.py contracts
python3 -B packs/web/test_extract.py
```

Run at the repository root, or supply the root through `NARU_ROOT`. The
`SurfaceTests`, `ValidationTests` and `BuildTests` unittest groups use synthetic
files and fake executables: they do not install packages or contact a registry.
Actual Node HTTP/runtime and offline npm evidence is recorded separately in the
repository verification document. Go static analyzers do not analyze Python or
JavaScript; these fixtures, runtime tests and independent review cover those
parts.

## Manual activation

Add `packs/web/pack.yaml` to the existing `packs` list, preserving other packs,
checks and modes. A new disposable example project can use:

```yaml
version: 1
default_mode: code
packs: [packs/web/pack.yaml]
modes: {code: [web.contracts]}
```

```sh
naru check web.contracts
naru baseline extract web.routes
naru baseline diff web.routes
naru baseline approve web.routes
# Repeat extract, diff and interactive approval for web.dependencies and web.env.
```

Review the complete diff before terminal approval. `web.contracts` verifies the
pipeline but does not enforce equality with an approved baseline. See the
[actual verification scope](../../docs/web-verification.md).

## Preparation and trust boundary

One nonblocking `.naru/state/web-build.lock` serializes this pack's builds.
The adapter copies only the root package/lock/env example, direct `.mjs` files
in scripts/src/test, and the message workspace manifest/modules. Source symlinks
and unsupported paths are rejected. Each copied file is at most 1 MiB, with
1024 files and 8 MiB total. README, generated node_modules/dist, private `.env`
and `.npmrc` are never copied or parsed.

The complete local-only lock and env grammar are checked before launching any
child. Every child, including version probes, receives a small explicit
environment: selected PATH, isolated HOME/TMPDIR, and fixed locale. Inherited
NODE_OPTIONS, npm_config variables, tokens and proxies are not forwarded.
Distinct empty npm user/global config files and a fresh cache accompany npm
version probes and `npm ci --offline --ignore-scripts --no-audit --no-fund`.
No lifecycle script, fallback install, registry dependency or network retry is
supported. A trusted selected executable may still access the filesystem or
network itself; this environment boundary is not an operating-system sandbox.

Node runtime tests run explicitly before the build/export script. They bind
only an ephemeral loopback port and close it. Every run uses a fresh source
copy, export directory and nonce. The child pipeline shares a 60-second deadline;
cleanup targets only its created process group with bounded TERM/KILL waits.
Unresolved cleanup fails. Same-user filesystem races and children deliberately
detaching their process groups are outside this isolation guarantee.

No mode reads a previous dist export. Failed extraction invalidates the current
candidate through NARU's existing behavior and leaves approved baseline bytes
unchanged. Absent declared tools are not-run for the build check; present but
wrong or failing tools produce failure. The adapter does not modify a baseline.

## Surface boundaries

Routes come from the same validated registry the HTTP app uses, not source text
matching. The supported methods are GET, POST, PUT, PATCH, DELETE, OPTIONS and
HEAD. Paths are literal ASCII slash-separated segments; dynamic patterns,
percent encoding, dot segments, query strings and fragments are unsupported.
IDs are `METHOD:/path`; zero routes represents complete removal. Export envelopes
and normalized stdout are bounded to 1 MiB, with at most 1024 routes. Missing,
duplicate, unknown, wrong-type and wrong-nonce output fails. Handlers, response
bodies, request IDs and tool metadata do not enter surface values.

Dependencies represent the root package and the single local message workspace.
Root/workspace metadata and the exact workspace link must match the committed
npm-generated lock. A workspace version change with a stale lock fails even if
npm itself accepts it; regenerate the lock locally to observe the version diff.
Unknown package locations, remote/integrity records and alternate links fail.
Optional generated license/engines metadata is checked but not exported. Raw
resolved paths, URLs and supplied metadata are never included in error messages.

The env surface reads only `.env.example`, at most 64 KiB and 1024 unique keys.
LF/CRLF, blank lines and comments are supported. Other lines must be `KEY=value`;
keys use `[A-Za-z_][A-Za-z0-9_]*` and are limited to 124 characters so IDs
`env:KEY` fit the existing 128-character surface contract. Values are discarded
without interpolation or shell evaluation. Same-line quotes are allowed, while
unmatched leading quotes, trailing backslash continuation, duplicate keys,
export prefixes and control characters are rejected. This is a key inventory,
not a full dotenv parser. Diagnostics never quote the source line or value.

Changing only an env value first makes old input evidence stale, then produces
the same key surface after a successful fresh run. Renaming or adding a key
changes the surface. All child diagnostics remain private and ephemeral;
public failures use fixed messages rather than forwarding raw npm/Node stderr.
