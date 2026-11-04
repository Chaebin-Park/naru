# Spring Boot public-example pack

This repository-owned pack demonstrates four observed contracts without changing NARU's core. It is intentionally limited to `examples/spring-boot`, Spring MVC, and the explicit `contract`/`extra` profiles. It is not a universal Spring application scanner.

## Supported tools

| Tool | Contract | Evidence |
| --- | --- | --- |
| Spring Boot | Pinned 3.5.16, Spring MVC | Real public-example Maven smoke on macOS |
| Java | Example targets Java 21 | Existing JDK 21.0.9 used; select a compatible installed JDK with `JAVA_HOME` |
| Maven | 3.6.3+ | 3.9.16 used |
| Python | 3.9+, standard library and POSIX `flock` | Existing Python 3 on macOS; Linux runtime pending |
| NARU | Pack/surface schema 1 | Go fixture tests plus extract/diff/approve smoke |

[Spring's requirements](https://docs.spring.io/spring-boot/3.5/system-requirements.html) define Java/build compatibility. The runtime route exporter uses Spring MVC's [RequestMappingInfo](https://docs.spring.io/spring-framework/docs/6.2.x/javadoc-api/org/springframework/web/servlet/mvc/method/RequestMappingInfo.html), rather than source-text matching. Unlike the [Actuator mappings endpoint](https://docs.spring.io/spring-boot/3.5/api/rest/actuator/mappings.html), the example needs no running HTTP service or exposed management endpoint. These are bounded support claims, not promises for every Spring version or JDK.

The example adds its own pinned Spring web/test Maven dependencies. NARU's Go module dependencies are unchanged. Maven may download build dependencies on first use; NARU installs no system tools. Offline execution requires a populated Maven cache. No wrapper binary, package-manager installation or repository settings are changed.

## Manual activation

From a working copy containing both the pack and public example, add this selection to `.naru/config.yaml`, preserving existing checks and modes:

```yaml
packs: [packs/spring-boot/pack.yaml]
```

Then run:

```sh
naru baseline extract spring-boot.api
naru baseline diff spring-boot.api
naru baseline approve spring-boot.api
# Repeat for spring-boot.routes, spring-boot.migrations and spring-boot.config.
```

Approval requires interactive terminal input; review the complete diff. `spring-boot.contracts` is a runnable build/export check that can be added to a mode manually. It checks build/extraction success, **not** equality with approved baselines. There is no implicit surface-to-mode policy. Do not claim that a mode containing this check alone enforces approved API equality.

The adapter receives NARU's root through `NARU_ROOT`; direct invocation defaults to the current directory. It writes only schema 1 JSON on stdout and sends build diagnostics to stderr. Ordinary extraction never approves a baseline.

## Surfaces and exclusions

| Surface | Observed data | Explicit limits |
| --- | --- | --- |
| `api` | Status, content type and JSON body for synthetic GET `/greeting` and `/extra`, per profile | Explicit probe allowlist; no arbitrary endpoints, credentials or production responses |
| `routes` | Application controller paths, HTTP methods, consumes/produces expressions (including negation), hash of header/parameter conditions, per profile | Only `example` package handlers; no WebFlux, functional routing, inherited package discovery or full OpenAPI schema |
| `migrations` | Filenames and SHA-256 of `src/main/resources/db/migration/*.sql` | No database connection, migration execution or live schema introspection; all files disappearing is an error |
| `config` | Source filenames and keys in `application*.properties` | No configuration values, defaults or environment dumps; YAML, escapes, continuations and duplicate keys are rejected |

The public API is deterministic; the adapter does not strip arbitrary dynamic fields. Changes to a response field remain visible. Production adapters must explicitly choose safe probes and record any normalization policy. Build logs may contain application output and remain local sensitive material; do not publish them automatically.

For API/routes, each run takes a shared pack build lock, performs `mvn clean test` and exports both profiles to a fresh temporary directory. Missing, empty, oversized, duplicate, malformed, mismatched-profile or failed exports cannot reuse a previous `target` artifact. Concurrent pack builds are refused. A failed resource read also fails extraction. The lock coordinates this adapter only; other Maven invocations and hostile same-user changes are not isolated. NARU's outer timeout/cancellation and candidate recovery rules still apply.

Adapting to another project requires changing the project path, package/probe/profile allowlists, manifest input globs and exporter together, then reviewing new baselines. Do not widen claims by only changing the pack name. Keep generated exports outside the pack directory, because every pack file participates in its fingerprint.

## Tests

```sh
python3 -B packs/spring-boot/test_extract.py
# Full real extraction (requires Java/Maven, may fetch Maven artifacts):
python3 packs/spring-boot/extract.py api
python3 packs/spring-boot/extract.py routes
python3 packs/spring-boot/extract.py migrations
python3 packs/spring-boot/extract.py config
```

`internal/cli/testdata/spring-boot/{normal,broken}` contains synthetic API-removal and route-change fixtures. Resource tests mutate SQL and key names while proving values are excluded. Adapter tests reject missing/invalid exports, failed/timed-out builds and concurrent builds. Default Go tests invoke these Python fixture tests; they do not run Maven or prove Java runtime compatibility. The real CLI smoke separately extracted/approved all four synthetic baselines, saw unchanged results, then detected a route/API removal, migration edit and key rename. No project baseline was approved by that test.

The real Maven tests also assert that changing negated media conditions changes the exported route contract; media-type-only getters would silently omit those conditions.
