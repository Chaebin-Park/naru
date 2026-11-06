# Clean-checkout Web CI smoke

NARU-020 remote failure/recovery acceptance was observed on 2026-11-06 after maintainer push approval. The exact revisions and runs are recorded below.

The workflow `.github/workflows/ci-smoke.yml` builds NARU and exercises the existing public Web pack on Ubuntu 24.04, Go 1.23.x and Node 22.22.2 (the pack also requires npm 10.9.7). It uses read-only repository permissions, disables persisted checkout credentials and dependency caches, pins action commits, and records the exact Git revision and tool versions. It runs on pushes to `main` and `ci/naru-020-*`, or manual dispatch once available remotely.

The action inputs follow the official [checkout](https://github.com/actions/checkout), [setup-go](https://github.com/actions/setup-go) and [setup-node](https://github.com/actions/setup-node) documentation. Pinned commits correspond to the v7 tags inspected on 2026-11-06. Remote setup may download the declared toolchains on the disposable runner; no local tool installation is performed.

## Source and evidence boundary

`scripts/ci-web-smoke.py` resolves a commit, archives only `packs/web` and `examples/web`, rejects unsafe archive entries and builds a fresh temporary sample root. Local `.naru` receipts, logs, relay submissions and ignored build outputs are not copied. Its generated sample configuration selects `web.contracts`.

The runner executes `naru check --mode ci --evidence ci`; status is diagnostic only and cannot replace the execution exit code. The Web adapter performs isolated offline local-workspace preparation, HTTP tests and deterministic exports. No registry runtime dependency, deployment or persistent service is required.

## Local reproduction

With the already-approved tool versions available:

```sh
go build -o /tmp/naru-ci ./cmd/naru
python3 scripts/ci-web-smoke.py /tmp/naru-ci --revision HEAD
```

On 2026-11-06, this command passed against the committed public sample. A disposable clone with an added duplicate `APP_LABEL` key committed to `examples/web/.env.example` returned exit 1. The real working copy was not mutated for the negative sample. `TestCIExecutionFreshness` also proves that matching local passing evidence cannot mask a failing actual CI execution when an external condition changes; `TestRelayWorkflow` covers independent CI execution after relay.

These local observations supplement, but do not replace, the remote evidence below.

## Remote acceptance procedure

1. Publish an isolated `ci/naru-020-*` branch containing the reviewed workflow and an intentional duplicate-key sample revision.
2. Record its workflow URL, exact SHA, tool versions and failing Web-check step/exit. A setup or build failure does not prove the intended sample rejection.
3. Fast-forward that same test branch to a commit restoring the original sample. Record its successful workflow and exact SHA.
4. Confirm no local receipts or relay artifacts were uploaded and preserve the command exit results.
5. Add the observed evidence here, rerun the task verification gate and close NARU-020 only after the remote acceptance criteria are met.

The requested branch publication/Actions execution is separate from release publication, repository-settings changes and actual Git pre-push integration acceptance.

## Observed remote acceptance (2026-11-06)

The approved branch `ci/naru-020-20261106` was pushed in two sequential revisions:

| Revision | Run | Observed result |
| --- | --- | --- |
| `ee647ce80ad51f4d7398e5d76096f55c603b441a` | [Failure run](https://github.com/Chaebin-Park/naru/actions/runs/42181560038) | Setup/build passed; duplicate `APP_LABEL` sample failed `web.contracts`, exit 1, `satisfied: false`, provenance `ci`. |
| `93ddafb0d16bcd4a01d45b88962e3b2afb5dd4c5` | [Recovery run](https://github.com/Chaebin-Park/naru/actions/runs/42181653446) | Restored sample passed `web.contracts`, exit 0, `satisfied: true`, provenance `ci`. |

Both clean Ubuntu 24.04 runners built with Go 1.23.12 linux/amd64 and used Node 22.22.2, npm 10.9.7 and Python 3.12.3. The workflow executed the committed source archive through `python3 scripts/ci-web-smoke.py "$RUNNER_TEMP/naru" --revision "$GITHUB_SHA"`. Neither run uploaded artifacts; local receipts and relay submissions were absent from the archived inputs. The successful check evaluated nine input files and completed in approximately 860 ms.

This establishes the bounded public Web smoke and minimum-Go build/runtime path, not the full Go test suite on Go 1.23, Windows runtime support, deployment, or a release. The intentional failing sample and restoration remain on the test branch; main's sample was unchanged.
