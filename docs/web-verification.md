# Web pack verification

NARU-017 covers the public `examples/web` Node HTTP application and local
workspace dependency through `packs/web`. Its static route registry and fixed
local dependency graph do not establish support for arbitrary Web frameworks.

## Environment and provenance

Implementation used Node 22.22.2, npm 10.9.7, Python 3 and Go 1.27.3 on macOS.
Every sample subprocess, including version probes, ran with isolated HOME,
npm configuration and cache, without inherited NODE_OPTIONS, npm configuration,
tokens or proxy variables. Existing tools were used; there were no system
installations, global settings changes, registry dependencies or deployments.

The initial actual npm-generated v3 lock, offline workspace installation,
loopback HTTP tests and fresh export all succeeded. The runtime imported the
workspace package, independently checked its expected response and served the
shared registry. Invalid nonce, duplicate routes, symlink destinations and
nonempty output directories were rejected without overwriting prior output.

These runs used a temporary copy of the worktree based on `08bd05e`, before the
implementation commit. Local evidence preserves the exact commands and source
hashes. The final task ledger binds the implementation revision and isolated
repository verification. Source/runtime agreement must be checked separately
from a successful fixture suite.

## Actual CLI smoke

The disposable smoke completed 55 recorded commands/approvals. It used the
already-built Android-task NARU binary; this task changes no Go production
behavior. All runtime source hashes matched the worktree after restoration.
The only copied-file difference was the pack README's activation instructions.

| Scenario | Observed result |
| --- | --- |
| Offline npm ci, actual HTTP tests and Node export | Exit 0 with fresh isolated configuration and cache |
| Initial routes, dependencies and env candidates | Extract 0, missing-baseline diff 1, explicit terminal approval 0 |
| Repeat all three surfaces | Unchanged diffs; exit 0 |
| Remove GET /message | Actual loopback request returned 404; route removal diff 1; restored diff 0 |
| Workspace version changed with stale lock | Extract 1 and failed candidate; no old success reused |
| Regenerate lock using offline npm 10.9.7 | Selected workspace version 1.0.1 produced diff 1; restored manifest/lock produced diff 0 |
| Rename APP_LABEL to APP_TITLE and add SMOKE_FLAG | Expected env:KEY additions/removal; diff 1; restored diff 0 |
| Change only a dummy environment value | Previous candidate rejected as stale; fresh extraction had the same key digest and unchanged diff 0 |
| Malformed env containing a sentinel | Extract 1; sentinel absent from output and runner diagnostics |
| Broken build containing a sentinel, with an old valid export present | Extract 1; failed candidate, no output reuse or sentinel disclosure |
| Final restoration | Three unchanged diffs and web.contracts check 0 |

Approved baseline bytes stayed unchanged throughout mutation, failure and
restoration. Local logs preserve exact commands, source hashes, raw synthetic
exports, a sentinel scan and the final pass summary. Private paths and raw logs
are not committed. Environment values are not copied into this public record.

## Automated and review boundaries

Default tests use bounded Python fixtures and Node loopback tests with local
workspace links. They do not fetch npm packages. Actual npm offline preparation
and CLI baseline mutation evidence are separate completion requirements.

The repository's static tools scan Go code and Go vulnerabilities. Python syntax,
Node syntax/runtime tests and adversarial parser fixtures are additional checks;
the Go tools are not claimed to scan Python or JavaScript vulnerabilities.

Only `.env.example` is read for environment key inventory. Its dummy values are
discarded, not included in surfaces or errors. Neither private `.env` files nor
process environment values are inventory inputs. Build and package commands are
trusted example code, not an isolation sandbox for hostile source programs.

## Limits

Only the pinned Node/npm versions, static route grammar and root-to-local
workspace lock graph were tested. Linux/Windows runtime, minimum Go 1.23,
external registry or git dependencies, deployments, dynamic routers and other
frameworks were not tested. Cross-platform Go vet is not runtime validation.
Actual Git push tests were not enabled.

The build check does not enforce approved baseline equality. Approvals in the
smoke tests apply only to disposable synthetic projects. Route surfaces contain
method/path, not response schemas; environment surfaces contain key names, not
values. See the [pack contract](../packs/web/README.md).
