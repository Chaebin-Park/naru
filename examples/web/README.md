# Local Node web contract example

This public sample uses Node 22.22.2, npm 10.9.7 and standard libraries. It has no registry dependencies. The HTTP runtime imports `naru-web-message` through npm's local workspace link. Its two initial routes are `GET /health` and `GET /message`.

The [web pack](../../packs/web/README.md) builds a fresh temporary copy, prepares workspace links offline, runs loopback HTTP tests and exports three reviewable surfaces:

- **Routes:** method and literal path from the same registry the HTTP server uses.
- **Dependencies:** root metadata and the local workspace version/relationship from the committed npm v3 lock, checked against both package manifests.
- **Environment:** key names from the synthetic `.env.example`, without values.

From the repository root, use the pack adapter to exercise the complete isolated pipeline:

```sh
python3 packs/web/extract.py contracts
python3 packs/web/extract.py routes
python3 packs/web/extract.py dependencies
python3 packs/web/extract.py env
```

Each invocation uses a fresh copy and empty npm cache, with `npm ci --offline --ignore-scripts --no-audit --no-fund`. The adapter supplies empty user/global npm configuration files and a small process environment; it does not copy project `.npmrc`, `.env`, `node_modules` or previous exports. It runs the reviewed Node test/build commands explicitly. No installation script, network package, global setting change or persistent service is needed.

For a prepared disposable copy, the underlying Node commands are:

```sh
node --test test/runtime.test.mjs
mkdir fresh-export
node scripts/build.mjs fresh-export 0123456789abcdef0123456789abcdef
```

These commands assume workspace links have already been created with the isolated offline npm command. The output directory must exist, be empty and not be a symlink. `web-export.json` contains a request ID, actual Node version and route descriptions; volatile metadata does not enter the normalized route surface. The HTTP module exports `createApp()` but never opens a listener on import. Tests use `127.0.0.1` on an ephemeral port and close all connections when finished.

## Review exercises

Use a disposable copy when experimenting with baselines. Pack extraction alone does not approve a baseline or require approved equality.

1. Remove the `/message` entry in `src/routes.mjs`. Runtime tests confirm that path returns 404, and the route surface loses its entry.
2. Change `packages/message/package.json` version. The stale lock must fail. Regenerate the lock using the same isolated npm 10.9.7 setup and `npm install --package-lock-only --offline --ignore-scripts --no-audit --no-fund`; then the dependency surface changes. Do not hand-edit the lock to bypass validation.
3. Rename `APP_LABEL` in `.env.example`. The environment surface changes. Change only its dummy value: previous input evidence becomes stale, but successful re-extraction produces the same key surface.
4. Break the registry or build. Extraction fails even if a previous valid export exists. Restore the source before comparing again.

## Deliberate limits

This example is a bounded static registry, not a framework or deployment scanner. Paths are `/` or slash-separated ASCII letters, digits, dot, underscore and hyphen; dot segments, dynamic parameters, encoded paths, query/fragment patterns and trailing slashes are unsupported. Methods are explicit uppercase HTTP methods. Duplicate routes and invalid handlers fail. Handler code, response bodies and schemas are outside the surface. Add an independent expected response in the runtime test when adding a route.

The dependency parser supports only this root-to-local-workspace graph. Registry/git dependencies, escaped links and arbitrary package graphs fail. The runtime does not use `.env.example`; it is a public inventory fixture, not a configuration loader. Environment extraction accepts simple single-line `KEY=value` declarations, comments and blank lines. Duplicate keys, `export` prefixes and multiline/continuation forms fail. Neither process environment nor private `.env` files are part of this inventory. Errors must not include source lines or values.

Environment surface identities use `env:KEY`; values contain only `{ "name": "KEY" }`. Keys may start with an underscore and have at most 124 characters, keeping the prefixed identity within NARU's 128-character limit.
