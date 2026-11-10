# Linux status collection (E03a / NARU-044)

The synthetic `scripts/measure-status.py` workload supports Linux and macOS. Linux uses `scripts/linux_status_measure.py`; macOS retains `/usr/bin/time -l`. Warm-cache still excludes one warmup; first-run includes exactly one first status after receipt preparation. Preparation reads inputs, so first-run is not cold. All cold-cache requests still exit 2 before preparation. E03b must separately implement and verify reset/probe/start ordering and reader controls before enabling a cold backend.

```sh
go build -o /tmp/naru-measure ./cmd/naru
python3 scripts/measure-status.py /tmp/naru-measure --samples 2 --checks 2 --files 4 --bytes 4096
python3 scripts/measure-status.py /tmp/naru-measure --cache-mode first-run --checks 2 --files 4 --bytes 4096
```

Linux wall time uses monotonic nanoseconds from immediately before child launch through observed `wait4` completion. A 1 ms polling interval allows a 120-second timeout without installing a process-wide signal handler. Launch overhead, scheduling and completion-detection delay are included. Output is collected in temporary files and decoded after timing, avoiding pipe deadlock. This collector adds overhead and does not measure isolated evaluator CPU time.

The controller exclusively reaps its selected child and converts Linux `ru_maxrss` KiB to bytes. This is the kernel's peak accounting for that waited child, which can include inherited/waited-descendant accounting; it is not the sum or simultaneous maximum RSS of a process tree. A lost reaping owner aborts without signaling a potentially reused PID. On timeout/interruption, the controller kills its created process group and reaps the direct child. It is not a sandbox for hostile binaries, detached descendants or uninterruptible kernel waits.

A nonzero/signaled exit, timeout, oversized output, unavailable collector or invalid RSS rejects the run. Status JSON must report boolean `satisfied: true` and exactly the expected unique check names in `pass` state. JSON/validation errors abort instead of yielding a successful sample. The helper emits its final summary only after all requested samples pass; it does not silently discard failures or retry them.

The result records `collector`, `wall_scope`, `rss_scope`, platform, binary SHA-256, cache label, workload size, warmups and raw sample rows. `go` is the installed measurement-host toolchain version, or null when unavailable; it does not attest the binary's build toolchain. Binary hashing and fixture preparation precede measurement. The prepared workload manifest described below binds samples to observed files. Reader control is optional and described below.

Do not compare these Linux values directly with historical macOS time-wrapper measurements or treat a small smoke as P11's thirty-sample workload. No Linux performance threshold is claimed here. The UTM reset control in [NARU-043](linux-cache-probe.md) remains separate; this task's native Linux coverage comes from the CI runner, not a status run in that VM.

## Verification

`TestStatusMeasurementHelper` runs portable label, validation, conversion and reaping-ownership rejection cases. On Linux it also exercises real child success, exit failure, signal death, timeout, oversized output and launch failure. That real-child case is explicitly skipped on other systems. `TestLinuxStatusMeasurementSmoke` builds the current binary and measures warm-cache and first-run with four files, 4096 bytes and two required checks. Linux CI proves its wait4 path; macOS exercises the retained time-l path; Windows skips the smoke because neither RSS collector applies.

The Linux [wait4 manual](https://man7.org/linux/man-pages/man2/wait4.2.html) specifies selected-child resource accounting; [getrusage](https://man7.org/linux/man-pages/man2/getrusage.2.html) documents Linux peak-RSS units and process-tree limitations. Python's [wait4 API](https://docs.python.org/3.9/library/os.html#os.wait4) exposes the returned resource usage. These semantics do not make measurements from different operating systems directly comparable.


## Prepared workload provenance (E03b-1 / NARU-045)

The warm/first-run helper hashes the selected binary before `check`, then captures its SHA-256 again with the prepared synthetic project. A mismatch rejects preparation. The project manifest inventories **every regular file**, including `src` inputs, `.naru/config.yaml`, prepared receipts and other generated evidence; empty files are included. Each entry has a project-relative path, byte size and SHA-256. `provenance.manifest` also records the binary size/digest and schema 1. Its `manifest_sha256` hashes UTF-8 JSON with sorted keys, compact separators and Python's default ASCII escaping. Each accepted `samples_data` row carries that same digest; the existing top-level binary SHA-256 remains.

The POSIX helper requires an owned private project root, owned subdirectories/files without group/other write permission, and regular singly linked files. It rejects leaf symlinks, directory symlinks, FIFOs and other non-regular files. Use an actual binary file rather than a binary symlink; canonicalizing trusted ancestors such as macOS `/tmp` does not resolve the selected leaf. Installed binaries may have a different owner but must also be regular, singly linked and without group/other writers. An unusual permission layout or unsupported descriptor API fails rather than silently removing provenance checks.

After preparation, a pre-sample check compares directory membership and file identity (device/inode, owner/mode, link count, size, mtime and ctime). It enumerates pinned directories and stats paths, but does **not** open/read file content. Reads may update atime, which is deliberately excluded. File content hashes are recomputed after each status, outside its reported wall time; any mismatch rejects the entire run before a JSON summary is printed. This includes a successful status whose input, binary, configuration or receipt changes. Post-sample hashing also warms cache pages, which is compatible with these warm/first-run labels; it cannot be inserted before a future cold sample without another verified reset.

Only directory descriptors remain open between checks, and they are not inherited. Files are hashed one at a time so the 10,000-file workload does not require 10,000 descriptors. Directory/root replacement, file addition/removal/replacement, changed metadata or content, and read/stat failures reject the run. Descriptor cleanup also runs when preparation fails.

The manifest can make output substantially larger than the previous timing summary. Save full JSON if the inventory is needed for later verification; its paths are relative and it contains hashes rather than receipt/log contents. Generated execution IDs/evidence vary across preparations, so a manifest digest is not a cross-run reproducibility claim, a signature, or a build-toolchain attestation. The declared binary SHA-256 identifies bytes observed around preparation and collection, not authenticated build provenance.

This is an observational binding for a controlled synthetic fixture. It does not prevent a hostile same-user/root actor from modifying and restoring files between observations; metadata and content observations are not filesystem snapshots or proof that status saw a single immutable version. The optional E03b-2 guard below checks readers during collection; E03b-3 still owns reset/probe/start ordering. `cold-cache` remains rejected before any preparation. No VM reset or performance threshold is enabled here.

`TestStatusProvenanceHelper` runs manifest/digest, metadata-only validation, input/config/receipt/binary mutation, same-content replacement, path/root replacement, unsafe-file, cleanup and bounded-descriptor tests. A fake executable also demonstrates that valid passing status JSON cannot rescue changed inputs/receipts or a binary changed during preparation. POSIX tests run on macOS/Linux and skip on Windows. `TestLinuxStatusMeasurementSmoke` checks actual warm/first-run status output and recomputes the sample's manifest digest. A macOS pass is not native Linux runtime evidence.


## Optional Linux reader guard (E03b-2 / NARU-046)

`--reader-guard` requires Linux, root, permission-capable fanotify and complete
procfs visibility in an already approved isolated environment. Use it only for
this helper's freshly created private synthetic fixture and a trusted binary.
Do not elevate an untrusted executable. Nothing installs privileges or resets
caches. Without the flag, the existing warm/first-run path is unchanged.

```sh
# In the approved isolated Linux environment, with an approved root session:
python3 scripts/measure-status.py /path/to/trusted/naru --reader-guard --files 4 --bytes 4096 --samples 1
```

After preparation and before each timed launch, `ReaderGuard` marks every pinned
fixture directory for direct-child OPEN/ACCESS/OPEN_EXEC permission events, plus
the selected binary itself. Directory marks avoid a mark/descriptor per input.
The guard then examines visible processes' `/proc/<pid>/fd` and `maps` for the
prepared files and binary by device/inode. An existing descriptor or mapping,
restricted procfs, malformed map or inspection error blocks collection. Only
processes/descriptors observed to have disappeared may be skipped. The controller
retains directory descriptors only; subprocesses use `close_fds` and no exported
fixture descriptors. The default fanotify PID mode reports TGID, allowing the
selected Go process's threads without authorizing its child processes.

A fixed `/bin/sh` handoff waits on a pipe before executing the binary with the
same TGID. This lets the collector learn its PID before an executable permission
event can block `Popen`. The synchronous collector owns monitoring and reaping;
there is no separate monitor thread/process whose death could go unnoticed.
Only the selected TGID is allowed during collection. Unexpected readers receive
DENY and invalidate the run. Overflow, malformed/unsupported events, monitor EOF,
read/write/close failures, a short response, an undrained event storm, no observed
permission event, child failure, timeout or terminal queued events invalidate the
run. Event and group descriptors close on error; the collector kills its created
process group and reaps its direct child on monitoring failure or timeout.
No partial successful JSON summary is printed.

Successful output and each sample row record `reader_guard: fanotify-tgid-v1`;
unguarded output records `none`. Wall time and waited-child peak RSS include the
shell handoff and monitoring overhead, so they must not be compared as the same
protocol as historical unguarded measurements. Setup/procfs inspection and
post-sample provenance hashing remain outside reported timing. Marks are removed
before controller hashing to avoid authorizing the controller as a second reader.

This is a controlled measurement protocol, not a security sandbox or an immutable
snapshot. Procfs enumeration is not atomic; hidden namespaces, hostile privileged
processes, transient reads/mappings and file replacement races are outside its
proof. Already opened descriptors/mappings are checked because fanotify alone
cannot revoke mmap access. Other processes must not be allowed to create racing
unobserved access in the isolated environment. Closing a fanotify group can allow
pending permission requests: a rejected sample stays rejected even if such an
open later succeeds. New/replaced files and changed directory membership are
also rejected by post-sample provenance. This task does not guarantee absence of
readers before/after the guarded interval, cache residency or reset/probe/start
ordering; **cold-cache remains rejected**.

`TestLinuxReaderGuardHelper` runs portable protocol/failure tests (native wait4
cleanup only on Linux). Privileged kernel acceptance is a separate required Linux
CI step, `python3 -B scripts/test_linux_reader_guard.py --native`, using a test-built
binary via `NARU_READER_TEST_BINARY`. Native mode fails rather than skipping when
Linux/root/support is unavailable. It tests real threaded access, existing fd and
mmap rejection, actual external-reader DENY, timeout/failure recovery and actual
warm/first-run status. Portable mocked events alone do not prove fanotify support.
The [fanotify overview](https://man7.org/linux/man-pages/man7/fanotify.7.html),
[initialization](https://man7.org/linux/man-pages/man2/fanotify_init.2.html) and
[mark API](https://man7.org/linux/man-pages/man2/fanotify_mark.2.html) document
permission replies, TGID reporting, CAP_SYS_ADMIN, mmap limitations and pending
request behavior on group close.
