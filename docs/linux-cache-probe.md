# Linux input-page residency experiment

NARU-043 adds a read-only experiment helper, `scripts/linux_cache_probe.py`. It does not change `naru` commands or enable cold-cache measurements. The existing measurement helper still refuses `--cache-mode cold-cache`.

`Fixture` pins a private, owner-only flat directory and nonempty, single-link regular files before reset. Open descriptors and device/inode/owner/mode/link-count/size/mtime/ctime identities bind repeated probes to those files. File-set changes, replacements, symlinks, unexpected ownership, externally writable files and mutations observed during a probe are rejected. Preparation must establish content hashes before reset; this helper deliberately does not hash or read input bytes during snapshots. It does not defend against a hostile same-user process or provide an atomic filesystem snapshot.

On Linux, `resident_pages` maps each file with `PROT_READ | MAP_SHARED`, without `MAP_POPULATE` or accessing the mapped bytes, queries `mincore`, then unmaps it. An unsupported platform, mmap/mincore/munmap error, impossible page count or invalid residency count is an error. A snapshot includes total/resident pages and monotonic start/end timestamps. `require_nonresident` rejects empty/partial/resident results; it does not turn zero residency into performance evidence.

Run the POSIX rejection suite (Linux or macOS) with:

```sh
python3 -B scripts/test_linux_cache_probe.py
go test ./internal/cli -run '^TestLinuxCacheProbeHelper$' -count=1
```

Windows skips this suite because it requires POSIX descriptor and permission semantics. Injected probes in these tests exercise failure decisions, not kernel behavior. They cover zero/partial residency, invalid counts, unavailable probes, unsafe input types/ownership, file replacement/mutation and unsupported systems. The helper contains no cache-reset CLI, privileged subprocess or `/proc/sys/vm/drop_caches` write.

## Separate isolated-guest control

A separately controlled disposable Linux VM is required for an actual reset trial. Before sending commands, the local controller checks the selected clone identity and running state, no NIC and no folder sharing; the operator has already verified clipboard/USB redirection are disabled. Neither a guest root check nor a user-provided UUID alone proves isolation. Do not run a reset in the personal host, Docker Desktop's shared Linux VM or a production guest.

The integration controller prepares a synthetic file on the guest's ordinary disk filesystem, fixes its content identity and opens `Fixture` before reset. It then:

1. Observes all pages resident after preparation and verifies this warm result is rejected as nonresident.
2. Flushes the fixture descriptors with `fsync`, runs `sync`, then requests clean page-cache eviction with value 1 inside that verified guest only. Open/write/close failures abort the trial.
3. Takes two consecutive read-only probes; both must report zero resident pages. This negative control also detects a probe which itself faults the input into cache.
4. Re-reads and verifies the synthetic content, then requires all pages resident again. An always-zero probe cannot pass this positive control.
5. Preserves kernel, boot identity, helper SHA-256, content hash, reset timing and raw snapshots privately, then removes its temporary fixture. Guest identifiers and private filesystem paths are not public fixtures.

No retry silently converts a failed reset or a partially resident result into success. Zero residency after reset also excludes remaining dirty input data pages at that snapshot; this experiment does not separately report guest-wide dirty-page counters. There is no status timing or thirty-sample performance conclusion here.

`mincore` gives a point-in-time observation. Other readers, readahead or later activity can change residency immediately afterward. Two zero probes are a control, not proof of absence of all intervening or future readers. The [controller-only status protocol](linux-status-measurement.md#controller-only-input-cold-collection-e03b-3--naru-047) now binds input identities, reader control and the fixed probe-to-start gap. Its actual reset remains confined to a separately host-verified isolated guest; ordinary CLI cold requests remain blocked. Guest RAM page-cache results say nothing about host, hypervisor or storage-controller cache temperatures.

## Observed guest control (2026-11-10)

The isolated Rocky Linux 9.8 aarch64 guest, kernel `5.14.0-687.12.1.el9_8.aarch64`, ran the controller with Python 3.9.25 on XFS. Its one synthetic file contained 1,048,577 bytes (257 pages). The controller payload's length and SHA-256 were checked after console transfer, and its execution repeated the hash check before any reset. An earlier corrupted console transfer failed decoding and was not counted as an executed control.

| Observation | Resident / total pages |
| --- | --- |
| Prepared warm input | 257 / 257 |
| First probe after guest sync/reset | 0 / 257 |
| Second probe without reading input | 0 / 257 |
| Content-verified re-read | 257 / 257 |

All controller assertions passed and the console returned without a cleanup error. The tested helper SHA-256 was `a51ee10832ab609d60aa6e1c45b276a14188154bcdf1105f5a1ed474400bd5ef`. Guest raw JSON, boot identity and console screenshots remain private; the results above were read from the console, not collected by the portable test suite or transferred as a raw JSON artifact. The original VM stayed stopped. The clone API and clone-specific runtime disk path established the selected guest, but the original disk hash and clone backing chain were not directly inspected.

This is one synthetic residency/reset control, not a status benchmark or cold-performance attestation. The snapshot, reader and storage-cache limitations above still apply. E03 must establish measurement ordering and input/reader controls before P11 can collect warm/cold samples.

## Sources

The Linux [mincore manual](https://man7.org/linux/man-pages/man2/mincore.2.html) defines the residency bitmap and its snapshot limitation. The kernel's [drop_caches documentation](https://docs.kernel.org/admin-guide/sysctl/vm.html#drop-caches) distinguishes page-cache eviction from inode/dentry reclamation, explains dirty data and sync, and warns of system-wide performance effects. These semantics motivate the isolated-guest boundary; they do not certify this controller or a particular sample.
