# Linux status collection (E03a / NARU-044)

The synthetic `scripts/measure-status.py` workload supports Linux and macOS. Linux uses `scripts/linux_status_measure.py`; macOS retains `/usr/bin/time -l`. Warm-cache still excludes one warmup; first-run includes exactly one first status after receipt preparation. Preparation reads inputs, so first-run is not cold. All public CLI cold-cache requests still exit 2 before preparation. Controller-only isolated-guest integration is described below; the public CLI does not provide a reset backend.

```sh
go build -o /tmp/naru-measure ./cmd/naru
python3 scripts/measure-status.py /tmp/naru-measure --samples 2 --checks 2 --files 4 --bytes 4096
python3 scripts/measure-status.py /tmp/naru-measure --cache-mode first-run --checks 2 --files 4 --bytes 4096
```

Linux wall time uses monotonic nanoseconds from immediately before child launch through observed `wait4` completion. A 1 ms polling interval allows a 120-second timeout without installing a process-wide signal handler. Launch overhead, scheduling and completion-detection delay are included. Output is collected in temporary files and decoded after timing, avoiding pipe deadlock. This collector adds overhead and does not measure isolated evaluator CPU time.

The controller exclusively reaps its selected child and converts Linux `ru_maxrss` KiB to bytes. This is the kernel's peak accounting for that waited child, which can include inherited/waited-descendant accounting; it is not the sum or simultaneous maximum RSS of a process tree. A lost reaping owner aborts without signaling a potentially reused PID. On timeout/interruption, the controller kills its created process group and reaps the direct child. It is not a sandbox for hostile binaries, detached descendants or uninterruptible kernel waits.

A nonzero/signaled exit, timeout, oversized output, unavailable collector or invalid RSS rejects the run. Status JSON must report boolean `satisfied: true` and exactly the expected unique check names in `pass` state. JSON/validation errors abort instead of yielding a successful sample. The helper emits its final summary only after all requested samples pass; it does not silently discard failures or retry them.

The result records `collector`, `wall_scope`, `rss_scope`, platform, binary SHA-256, cache label, workload size, warmups and raw sample rows. `go` is the installed measurement-host toolchain version, or null when unavailable; it does not attest the binary's build toolchain. Binary hashing and fixture preparation precede measurement. The prepared workload manifest described below binds samples to observed files. Reader control is optional and described below.

Do not compare these Linux values directly with historical macOS time-wrapper measurements or treat a small smoke as P11's thirty-sample workload. No Linux performance threshold is claimed here. The initial E03a collector was verified on the CI runner. The later isolated-guest reset/status smoke is recorded under E03b-3 below; the earlier standalone reset control is in [NARU-043](linux-cache-probe.md).

## Verification

`TestStatusMeasurementHelper` runs portable label, validation, conversion and reaping-ownership rejection cases. On Linux it also exercises real child success, exit failure, signal death, timeout, oversized output and launch failure. That real-child case is explicitly skipped on other systems. `TestLinuxStatusMeasurementSmoke` builds the current binary and measures warm-cache and first-run with four files, 4096 bytes and two required checks. Linux CI proves its wait4 path; macOS exercises the retained time-l path; Windows skips the smoke because neither RSS collector applies.

The Linux [wait4 manual](https://man7.org/linux/man-pages/man2/wait4.2.html) specifies selected-child resource accounting; [getrusage](https://man7.org/linux/man-pages/man2/getrusage.2.html) documents Linux peak-RSS units and process-tree limitations. Python's [wait4 API](https://docs.python.org/3.9/library/os.html#os.wait4) exposes the returned resource usage. These semantics do not make measurements from different operating systems directly comparable.


## Prepared workload provenance (E03b-1 / NARU-045)

The warm/first-run helper hashes the selected binary before `check`, then captures its SHA-256 again with the prepared synthetic project. A mismatch rejects preparation. The project manifest inventories **every regular file**, including `src` inputs, `.naru/config.yaml`, prepared receipts and other generated evidence; empty files are included. Each entry has a project-relative path, byte size and SHA-256. `provenance.manifest` also records the binary size/digest and schema 1. Its `manifest_sha256` hashes UTF-8 JSON with sorted keys, compact separators and Python's default ASCII escaping. Each accepted `samples_data` row carries that same digest; the existing top-level binary SHA-256 remains.

The POSIX helper requires an owned private project root, owned subdirectories/files without group/other write permission, and regular singly linked files. It rejects leaf symlinks, directory symlinks, FIFOs and other non-regular files. Use an actual binary file rather than a binary symlink; canonicalizing trusted ancestors such as macOS `/tmp` does not resolve the selected leaf. Installed binaries may have a different owner but must also be regular, singly linked and without group/other writers. An unusual permission layout or unsupported descriptor API fails rather than silently removing provenance checks.

After preparation, a pre-sample check compares directory membership and file identity (device/inode, owner/mode, link count, size, mtime and ctime). It enumerates pinned directories and stats paths, but does **not** open/read file content. Reads may update atime, which is deliberately excluded. File content hashes are recomputed after each status, outside its reported wall time; any mismatch rejects the entire run before a JSON summary is printed. This includes a successful status whose input, binary, configuration or receipt changes. Post-sample hashing also warms cache pages, which is compatible with these warm/first-run labels; it cannot be inserted before a future cold sample without another verified reset.

Only directory descriptors remain open between checks, and they are not inherited. Files are hashed one at a time so the 10,000-file workload does not require 10,000 descriptors. Directory/root replacement, file addition/removal/replacement, changed metadata or content, and read/stat failures reject the run. Descriptor cleanup also runs when preparation fails.

The manifest can make output substantially larger than the previous timing summary. Save full JSON if the inventory is needed for later verification; its paths are relative and it contains hashes rather than receipt/log contents. Generated execution IDs/evidence vary across preparations, so a manifest digest is not a cross-run reproducibility claim, a signature, or a build-toolchain attestation. The declared binary SHA-256 identifies bytes observed around preparation and collection, not authenticated build provenance.

This is an observational binding for a controlled synthetic fixture. It does not prevent a hostile same-user/root actor from modifying and restoring files between observations; metadata and content observations are not filesystem snapshots or proof that status saw a single immutable version. The optional E03b-2 guard below checks readers during collection; the E03b-3 controller API below adds ordered guest reset/probe/start evidence. Public CLI `cold-cache` remains rejected before any preparation. Provenance alone enables neither VM reset nor a performance threshold.

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
also rejected by post-sample provenance. This guard alone does not guarantee absence of readers before/after the guarded
interval, cache residency or reset/probe/start ordering. Public CLI cold-cache
requests remain rejected; the controller protocol below owns ordered collection.

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


## Controller-only input-cold collection (E03b-3 / NARU-047)

The public CLI still rejects `--cache-mode cold-cache` before preparation. There
is no reset command, JSON attestation, UUID flag or environment-variable unlock.
`scripts/linux_cold_status.py` instead exposes an internal protocol used by an
already host-verified disposable-VM controller. A `GuestSession` requires a live
trusted reset callback, expected boot UUID and controller context. Boot identity,
Linux and root are checked before and during collection; they do **not** prove
host isolation. The module never writes `drop_caches` and cannot decide whether
an arbitrary host is approved. The caller must verify the VM identity/running
state, original stopped, ordinary guest disk, no NIC/share/clipboard redirection
and transferred source/binary hashes before delivering the private reset code.
Do not use Docker Desktop's shared VM, the personal host or a production guest.

The controller calls `measure(args, session)` with cold mode and reader guarding;
an absent/wrong session, disabled guard or reset session on a non-cold mode is
rejected. This Python API is trusted orchestration, not a security boundary
against someone modifying the interpreter or calling arbitrary privileged code.
Cold mode uses zero warmups and repeats the complete reset protocol for **every**
requested sample. Guarded warm mode uses the same wait4 collector and shell
handoff, with exactly one excluded warmup and no reset.

Each cold sample follows this sequence:

1. Open the complete private flat `src` input fixture before reset. Inputs must
   be nonempty. A narrow reader-guard exception permits only the controller's
   exact, non-inherited, read-only prepared input probe descriptors. It does not
   exempt other processes, other descriptors or any existing mappings.
2. Arm the permission guard and observe every prepared input page resident.
   This warm positive control must also be rejected as nonresident.
3. `fsync` the probe descriptors, `sync`, recheck boot/monitor health, then invoke
   the controller's reset callback and require successful return, including its
   write/flush/close completion. A returned claimed-evidence object is rejected.
4. Check prepared metadata without reading content. Require two consecutive
   read-only probes covering every input/page, both with zero resident pages.
5. Unmap/close probe resources, reject queued external reader events, then start
   guarded status collection. At the collector's actual pre-launch timestamp,
   require a nonnegative second-probe-finish → collector-start gap of at most
   **100,000,000 ns (100 ms)**. Negative/excess gaps reject before `Popen`; this
   constant is not a user option. All sync/reset/probe/collection timestamps must
   also be ordered. The bound concerns collector launch, not first input read.
6. Reap successful status, drain the guard, close marks, recheck boot, and require
   every input page resident again after the actual status read. Then validate
   the expected passing check names/boolean `satisfied` and rehash provenance
   outside timing. Only a wholly successful invocation returns summary JSON.

`cache_transition` records scope, boot/controller context, manifest digest,
prepared/first/second/post-status probe snapshots, sync/reset boundaries,
collector boundaries, fixed bound and observed gap. `collection_complete` means
that collection/probes finished, not that later status/provenance validation
passed. The session retains attempted traces on failure; controllers must keep
rejected attempts and exceptions, not silently retry them into success. Every
returned row includes raw parsed status, exit code 0, manifest digest and reader
protocol; Linux rows also include monotonic collector timestamps and observed
permission-event count. Reset/probe work is excluded from reported status time.
Session context can contain private VM identifiers: keep raw results private and
publish only anonymized observations. Boot UUIDs and context are recorded claims
from the trusted controller, not signed attestations.

This protocol establishes **observed guest input-file-page nonresidency before
launch**, not a globally cold application, filesystem metadata, executable,
hypervisor, host or storage cache. Residency is a snapshot; later readahead,
unobserved privileged races and the existing provenance/reader limitations
remain. The 100 ms bound is an acceptance rule, not a performance SLA. Probe
FDs are pinned until the last probe, so the approved guest process needs enough
file descriptors for the workload; exhausting that limit rejects preparation.
Nothing installs tools or changes system limits automatically. Thirty-sample
P11 performance collection is a separate task.

`TestLinuxColdStatusHelper` runs ordering, fixed-gap, callback/boot, bad-residency,
coverage, monitor/provenance and cleanup failure decisions without a real reset.
`TestStatusMeasurementHelper` tests controller-only acceptance, sample/warmup
counts, raw status binding and rejection. Native Linux CI additionally invokes
`test_linux_cold_status.py --native` to exercise actual probe descriptors with the
guard, closing before child reads and rejecting writable/extra descriptors; it
**never resets OS caches**. Actual reset/status acceptance belongs to the
separately controlled isolated guest, not mocked tests or ordinary CI. See
[the probe controls and kernel sources](linux-cache-probe.md) for residency/reset
semantics and snapshot limits.


### Isolated-guest integration smoke (2026-11-10)

The approved disposable ARM64 guest executed source
`763286ec941d2323abb50f4e946ad3db38d8905b` with binary SHA-256
`9aba6ef44629b3b6575142bd7c07d630f81eecafbc52524999eb2f4c24ea95bd`.
The host checked the clone/running state, stopped original, absent NIC and
folder sharing before console delivery. Previously disabled clipboard/USB
redirection remained disabled. No cache reset ran on the host or CI. The
controller checked all 17 transferred file hashes, root/Linux/aarch64, the
observed boot identity and the XFS fixture filesystem before collection.
The guest used Rocky Linux 9.8, kernel `5.14.0-687.12.1.el9_8.aarch64` and
Python 3.9.25; no Go installation was needed. Its native cold-protocol suite
ran eight tests without skips.

The synthetic smoke used four files totaling 1,048,576 bytes, 256 input pages
and two required checks. Both modes used the guarded wait4 collector. Warm
mode excluded one warmup; cold mode used zero warmups and a fresh private
guest reset callback for each sample. No rejected collection was retried.

| Mode | Sample | Status wall seconds | Waited-child peak RSS bytes | Probe-to-start ns | Resident pages: prepared / first / second / post-status |
| --- | --- | --- | --- | --- | --- |
| warm-cache | 1 | 0.106576170 | 11,522,048 | n/a | n/a |
| warm-cache | 2 | 0.105585623 | 11,526,144 | n/a | n/a |
| cold-cache | 1 | 0.131526329 | 11,587,584 | 99,834 | 256 / 0 / 0 / 256 |
| cold-cache | 2 | 0.118943228 | 11,669,504 | 91,334 | 256 / 0 / 0 / 256 |

All four returned rows had exit code 0, `satisfied: true`, 95 observed
permission events and their prepared manifest digest. Both cold gaps were
nonnegative and below the fixed 100,000,000 ns bound. The controller returned
both complete results only after status/provenance validation. Full JSON
results remain in the isolated guest; private host evidence contains console
screenshots of the summaries and result hashes, not exported raw JSON.

This is integration acceptance, not P11's thirty-sample performance result or
an operating-system comparison. It does not establish cold host/storage
caches, an atomic filesystem snapshot or independent physical backing-chain
isolation. The existing observer, provenance and reader limitations apply.
