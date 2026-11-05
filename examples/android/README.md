# Android contract example

This synthetic Java app and local Android library exercise the `android` pack.
The example builds an APK and exports the actual merged debug manifest and
resolved `debugRuntimeClasspath` graph. It does not launch an emulator, install
an app, contact a service, or inspect a production project.

Use AGP **8.9.2**, Gradle **8.11.1**, JDK **17**, Android SDK Platform **35**, and
Build Tools **35.0.0**. Both modules use minSdk 23; the app targets SDK 35.
Select existing tools through `PATH`, `JAVA_HOME`, and `ANDROID_HOME`. No Gradle
wrapper binary or machine-specific `local.properties` is included. First use
may download the pinned Gradle plugin and its dependencies. SDK installation,
license acceptance, and global settings changes are separate operator actions.
`android.builder.sdkDownload=false` disables automatic SDK package installation;
missing packages must fail the build.

From the repository root, use the pack adapter:

```sh
python3 packs/android/extract.py manifest
python3 packs/android/extract.py dependencies
```

The adapter copies only approved sample files into a temporary build directory
and passes a fresh empty export directory and request ID to Gradle. Its build
lock is separate from the pack files. Build diagnostics go to stderr; stdout
contains only the requested NARU surface JSON. The pack README describes limits
and baseline commands. A successful build does not approve a baseline.

For an exporter-only check, copy this example to a disposable directory, create
an empty output directory outside that copy, and run:

```sh
gradle --no-daemon --console=plain --stacktrace \
  -p /path/to/disposable/android \
  -PnaruOutput=/path/to/empty/output \
  -PnaruRequestId=0123456789abcdef0123456789abcdef \
  clean :app:assembleDebug :app:exportNaruDebug
```

Choose a new request ID and empty output directory for each invocation. The
export task writes `debug-merged-manifest.xml` and `debug-export.json`. The task
uses the AGP `MERGED_MANIFEST` artifact provider and waits for `assembleDebug`;
it does not guess a build-intermediates path. Build failure must not be treated
as successful export, even if files from an earlier build exist.

The graph records selected components and adjacency. Project versions come from
Gradle's resolved module metadata where available; a same-build project fallback
is explicitly labeled `declared-project`. Requested selectors, repositories,
variant attributes and dependency constraints are outside this projection.
Composite builds and unsupported component kinds fail export. The task does not
claim Gradle configuration-cache support.

Useful independent mutations in disposable copies:

- Add `android.permission.ACCESS_NETWORK_STATE` to the library manifest. Its
  appearance in the app's merged output demonstrates manifest merging.
- Change the app activity's `android:exported` flag from `true` to `false`.
- Change the library's explicit version in `library/build.gradle`.
- Remove `implementation project(':library')` from `app/build.gradle`. The app
  intentionally uses no library class, so this remains a buildable graph change.

Restore each mutation before testing the next. Do not commit generated `.gradle`
or `build` directories, export files, SDK paths or local evidence. Runtime
verification records belong in the repository's verification documentation;
these instructions alone do not prove a successful build on another host.
