# Android debug contract pack

This pack builds the public Java app and local library in `examples/android`. It
exports two bounded surfaces from a fresh debug build:

- `android.manifest`: merged permissions and explicitly exported components.
- `android.dependencies`: selected debug runtime components and their outgoing
  dependency edges, including the app root and local project versions.

`android.contracts` performs the same build and validates both exports. It does
not compare or approve baselines. Use the existing `naru baseline` commands to
extract, compare and review each surface.

Select Python 3.9+, JDK 17, Gradle 8.11.1 and sdkmanager through `PATH`. Set
`JAVA_HOME` and `ANDROID_HOME` to installed tools. The SDK must already contain
platform 35 and Build Tools 35.0.0. The adapter checks these files before Gradle
starts and disables automatic SDK download. It does not invoke sdkmanager or
accept licenses. AGP 8.9.2 and Gradle plugin dependencies may need an initial
network download by Gradle. Supported hosts are macOS and Linux; no emulator,
device, signing credentials or Android UI operation is used.

```sh
python3 -B packs/android/extract.py manifest
python3 -B packs/android/extract.py dependencies
python3 -B packs/android/extract.py contracts
python3 -B packs/android/test_extract.py
```

Run from the repository root, or provide the root through `NARU_ROOT` as the
NARU runner does. Successful surface output is one schema-1 JSON document;
contracts emits no success output. Diagnostics go to stderr. Unit classes
`SurfaceTests`, `ValidationTests` and `BuildTests` can run separately. They use
synthetic files and fake executables, without Gradle, network or devices.

## Manual activation

Add `packs/android/pack.yaml` to the existing `packs` list in `.naru/config.yaml`.
Keep your other packs, checks and modes. For a new disposable example project:

```yaml
version: 1
default_mode: code
packs: [packs/android/pack.yaml]
modes: {code: [android.contracts]}
```

```sh
naru check android.contracts
naru baseline extract android.manifest
naru baseline diff android.manifest
naru baseline approve android.manifest
# Repeat extract, diff and interactive approval for android.dependencies.
```

Review the full diff before terminal approval. The build check does not enforce
surface equality and does not approve changes automatically.

## Build and evidence boundary

Each invocation takes a nonblocking lock at `.naru/state/android-build.lock`,
copies only the declared sample build files and app/library source trees into a
fresh temporary directory, and requests exports into another empty directory.
Symlinks in copied sources are rejected. At most 4096 files, 1 MiB per file and
16 MiB in total are copied. Existing build outputs, `.gradle` and local.properties
are not copied. An absolute workstation path is never written into the sample.

The build uses `--no-daemon`, a fresh request nonce, and a 240-second deadline.
Cleanup targets only the process group created by this invocation and uses
bounded TERM/KILL waits; unresolved cleanup is an error. `--no-daemon` alone is
not treated as a child-process cleanup guarantee. Hostile concurrent same-user
filesystem mutation and a child intentionally detaching its process group are
outside this adapter's isolation guarantee.

The exporter obtains `SingleArtifact.MERGED_MANIFEST` through the AGP artifact
provider and resolves `debugRuntimeClasspath` through Gradle component APIs.
The adapter reads exports only after a successful build. It verifies the exact
schema, tool family, variant, nonce, manifest hash and required files. Each
export and normalized surface is limited to 1 MiB. Graph bounds are 4096
components and 16384 edges. Invalid, duplicate, dangling or unreachable graph
entries are rejected. Module/project versions are values, so changes retain
stable item identities. Empty dependencies are valid when the library edge is
removed; the app root remains.

A failed build never reuses old exports. Surface extraction failure invalidates
the current candidate under NARU's existing surface contract while preserving
previously approved baseline bytes. An absent declared executable is not-run for
the build check; present tools with a missing SDK or unsupported version fail the
adapter. Successful unit fixtures do not prove an actual Android build; actual
versions and smoke results belong in the repository verification record.

## Manifest projection

Supported entries are `uses-permission`, activity, activity-alias, service,
receiver and provider. Component names are expanded against the merged package.
Every selected component must declare literal `exported=true` or `false`.
Permissions include optional positive maxSdkVersion. Aliases include their
normalized target; providers include sorted authorities and separately declared
read/write permissions. A component's general permission inherits the
application permission when absent; read/write values remain null when not
explicitly declared. This shows declared/inherited fields, not complete Android
runtime access-control inference.

Alternate permission forms, DTD/entities, unresolved placeholders, ambiguous
booleans, missing required fields and duplicate semantic identities fail.
Labels, intent filters, enabled flags, arbitrary metadata and other attributes
are excluded. Raw XML, build paths, nonce, timestamps and tool metadata are never
included in surface values. External toolchain bytes are not part of core input
hashing; the pinned family and declared sample scope do not establish universal
Android application compatibility.
