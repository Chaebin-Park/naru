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

The result records `collector`, `wall_scope`, `rss_scope`, platform, binary SHA-256, cache label, workload size, warmups and raw sample rows. `go` is the installed measurement-host toolchain version, or null when unavailable; it does not attest the binary's build toolchain. Binary hashing and fixture preparation precede measurement. This synthetic helper does not attest immutable input manifests or control other readers.

Do not compare these Linux values directly with historical macOS time-wrapper measurements or treat a small smoke as P11's thirty-sample workload. No Linux performance threshold is claimed here. The UTM reset control in [NARU-043](linux-cache-probe.md) remains separate; this task's native Linux coverage comes from the CI runner, not a status run in that VM.

## Verification

`TestStatusMeasurementHelper` runs portable label, validation, conversion and reaping-ownership rejection cases. On Linux it also exercises real child success, exit failure, signal death, timeout, oversized output and launch failure. That real-child case is explicitly skipped on other systems. `TestLinuxStatusMeasurementSmoke` builds the current binary and measures warm-cache and first-run with four files, 4096 bytes and two required checks. Linux CI proves its wait4 path; macOS exercises the retained time-l path; Windows skips the smoke because neither RSS collector applies.

The Linux [wait4 manual](https://man7.org/linux/man-pages/man2/wait4.2.html) specifies selected-child resource accounting; [getrusage](https://man7.org/linux/man-pages/man2/getrusage.2.html) documents Linux peak-RSS units and process-tree limitations. Python's [wait4 API](https://docs.python.org/3.9/library/os.html#os.wait4) exposes the returned resource usage. These semantics do not make measurements from different operating systems directly comparable.
