# Clean-checkout Web CI smoke

NARU-020 local preparation is implemented; **actual remote failing/corrected CI runs are pending maintainer approval**. Local success does not complete the CI acceptance criterion.

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

These are local observations. The Go 1.23 remote build, GitHub runner behavior and actual remote failure/recovery pair remain unverified.

## Remote acceptance to execute after approval

1. Publish an isolated `ci/naru-020-*` branch containing the reviewed workflow and an intentional duplicate-key sample revision.
2. Record its workflow URL, exact SHA, tool versions and failing Web-check step/exit. A setup or build failure does not prove the intended sample rejection.
3. Fast-forward that same test branch to a commit restoring the original sample. Record its successful workflow and exact SHA.
4. Confirm no local receipts or relay artifacts were uploaded and preserve the command exit results.
5. Add the observed evidence here, rerun the task verification gate and close NARU-020 only after the remote acceptance criteria are met.

The requested branch publication/Actions execution is separate from release publication, repository-settings changes and actual Git pre-push integration acceptance.
