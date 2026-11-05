# Repository-local packs

A pack is a versioned directory in the project repository. NARU reads selected packs; it never downloads, installs or updates them. Pack commands are trusted project shell code, like ordinary checks. Path validation is not a shell sandbox or protection against hostile concurrent filesystem changes.

## Select a pack

Place a manifest at `packs/<directory>/pack.yaml`, alongside its scripts and fixtures. Directory names use the existing check-name characters (letters, digits, dots, underscores and hyphens, starting with a letter or digit). Select it explicitly:

```yaml
version: 1
packs: [packs/dummy/pack.yaml]
default_mode: code
modes:
  code: [dummy.readme]
```

Copy the [dummy manifest](../internal/config/testdata/project/packs/dummy/pack.yaml) and provide a project `README.md` to try `naru check` and `naru status`. It uses a shell builtin check and declares a synthetic surface. Surface declarations are not mode checks and cannot satisfy a gate; use the [baseline commands](baselines.md) to extract and compare candidates; approval is separate.

`naru init` keeps its existing stack detection and does not silently activate packs. After init, review/copy a local pack, add `packs`, and explicitly include namespaced checks in the desired modes. No new init flags or automatic selection are introduced. Local checks can coexist with pack checks.

## Manifest schema 1

```yaml
schema: 1
id: dummy
version: 1.0.0
requires:
  tools: [sh]
  os: [darwin, linux]
supported_tools:
  sh: "POSIX-compatible shell; tested versions belong in the support table"
checks:
  readme:
    run: test -f README.md
    inputs: [README.md]
surfaces:
  contract:
    run: 'printf ''{"schema":1,"items":[]}'''
    inputs: [README.md]
    extractor_version: dummy-v1
```

- Required identity: schema 1, `id`, `version`. ID/version use check-name syntax and at most 64 bytes. This is a version label, not an enforced semantic-version constraint solver. At least one check or surface is required.
- Entries are named `<pack-id>.<local-name>`. Local names are at most 64 bytes and the combined name at most 128. Dots are allowed but never imply nested imports; any resulting collision is an error. A repeated pack ID, including selecting the same pack twice, is an error.
- Checks use existing `run`, `inputs`, `workdir`, `timeout`, and `requires`. Surfaces add required `extractor_version`, using the [surface protocol](surface-contract.md) identity syntax. Project config can also declare a `surfaces` map directly.
- Commands, inputs and workdirs are **project-root relative**, not manifest-directory relative. Pack workdirs must already exist inside the root and cannot traverse symlinks. Reference bundled scripts with paths such as `sh packs/dummy/extract.sh`.
- Common and entry tools are a deduplicated union. OS allowlists are intersected; an omitted list contributes no restriction. An empty intersection is invalid rather than unrestricted. Invalid/duplicate declarations are rejected before merging. Implicit `sh` still applies at execution time.
- `supported_tools` maps executable names to nonempty informational version descriptions (maximum 256 bytes). It does not probe versions or implicitly add a `requires` entry.
- Unknown keys, duplicate YAML keys, multiple YAML documents, unsupported schema, and namespace collisions with local declarations are errors. There is no override precedence or recursive pack loading.

`config.Load(root)` resolves packs and validates the combined configuration. Rootless `config.Parse` rejects pack references rather than guessing a root. Projects still require at least one check and a nonempty mode; surfaces alone are not a verification policy.

## Fingerprints and filesystem limits

The loader hashes the selected manifest path and every regular file under its pack directory, including names, content SHA-256 and executable bits. This digest salts each contributed check and surface definition. Manifest edits, script/fixture changes, version changes and path selection changes therefore invalidate relevant evidence even when their ordinary input globs do not select pack files. Other checks retain their existing fingerprint. Comments/whitespace in a pack file also invalidate evidence conservatively.

Place generated logs and candidates outside pack directories. A pack accepts at most 256 regular files and 16 MiB total file content; the nonempty manifest is at most 1 MiB. Symlinks in manifest paths or anywhere inside a pack, special files, absolute paths and traversal components are rejected. Limits prevent unbounded reads but are not a sandbox or an atomic filesystem snapshot. A pack root may live under a canonicalized project-root alias.

Pack contents are tracked by this fingerprint even if Git ignores them; to reproduce verification elsewhere, commit all required files. This does not make arbitrary external tool state part of the digest. `supported_tools` remains descriptive, not evidence that a tool version was verified.

## Support table template

Each real pack should publish a table with actual evidence; the dummy pack is only a fixture.

| Pack version | Tool/version | OS | Verification | Known limits |
| --- | --- | --- | --- | --- |
| `<version>` | `<exact tool version>` | `<OS/version>` | `<command, fixture and date>` | `<unsupported behavior>` |

Tests: `TestPackLoading`, `TestPackRejections`, `TestPackChangesInvalidateEvidence`, and `TestRepositoryPackCLI`. Runtime evidence here is macOS with the approved temporary Go toolchain. Windows/Linux vet is not runtime validation. Automatic upgrades, registries and all-language extraction remain out of scope.

## Public examples

| Pack | Observed surfaces | Runtime evidence and limits |
| --- | --- | --- |
| [Spring Boot](../packs/spring-boot/README.md) | API, routes, SQL hashes, configuration keys | Bounded Java 21/Spring MVC example; pack README records actual Maven smoke |
| [Android](../packs/android/README.md) | Debug merged manifest, resolved dependency graph | [Pinned Java/Gradle/SDK build and mutation checks](android-verification.md); no emulator or device |
| [Web](../packs/web/README.md) | Shared HTTP routes, local workspace lock, environment key names | [Pinned Node/npm offline build and mutation checks](web-verification.md); no universal framework discovery |

Each example requires its matching sample sources and explicit pack selection.
A build check proves extraction can run; it does not enforce equality with an
approved baseline. Tool prerequisites check executable availability, while the
adapter validates its narrower supported tool and input contract.
