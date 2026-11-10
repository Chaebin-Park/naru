"""Linux launch-through-reap wall time and wait4 peak RSS; never resets caches."""
import math
import os
import platform
import signal
import subprocess
import tempfile
import time


def rss_bytes(kib):
    if type(kib) is not int or kib <= 0:
        raise ValueError("invalid Linux wait4 peak RSS")
    return kib * 1024


def collect(command, cwd, timeout=120, reader_guard=None):
    if platform.system() != "Linux" or not hasattr(os, "wait4"):
        raise RuntimeError("Linux wait4 collection requires Linux")
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError("measurement timeout must be positive")
    # Files avoid pipe deadlock while this function exclusively owns wait4/reaping.
    with tempfile.TemporaryFile() as output, tempfile.TemporaryFile() as errors:
        started = time.monotonic_ns()
        # A fixed shell waits for release so Popen cannot deadlock on a permission
        # event during exec before the controller learns the child TGID. exec keeps
        # that TGID. Both shell handoff and monitoring overhead are timed/accounted.
        launch = (["/bin/sh", "-c", 'read -r release && exec "$@"', "naru-guard", *command]
                  if reader_guard is not None else command)
        child = subprocess.Popen(launch, cwd=cwd,
                                 stdin=subprocess.PIPE if reader_guard is not None else subprocess.DEVNULL,
                                 stdout=output, stderr=errors, start_new_session=True, close_fds=True)
        try:
            if reader_guard is not None:
                reader_guard.service(None)
                child.stdin.write(b"go\n")
                child.stdin.close()
            while True:
                if reader_guard is not None:
                    reader_guard.service(child.pid)
                pid, status, usage = os.wait4(child.pid, os.WNOHANG)
                if pid:
                    child.returncode = os.waitstatus_to_exitcode(status)
                    break
                if time.monotonic_ns() - started >= timeout * 1_000_000_000:
                    raise subprocess.TimeoutExpired(command, timeout)
                time.sleep(.001)
        except ChildProcessError as error:
            # Reaping ownership was lost: do not signal a potentially reused PID.
            child.returncode = 255
            raise RuntimeError("measurement child reaping ownership lost") from error
        except BaseException:
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                if child.stdin is not None and not child.stdin.closed:
                    child.stdin.close()
            finally:
                _, status, _ = os.wait4(child.pid, 0)
                child.returncode = os.waitstatus_to_exitcode(status)
            raise
        finished = time.monotonic_ns()
        if reader_guard is not None:
            reader_guard.service(None)  # Reject events left over after selected-child exit.
        output.seek(0)
        errors.seek(0)
        stdout, stderr = output.read(1_048_577), errors.read(1_048_577)
        if max(len(stdout), len(stderr)) > 1_048_576:
            raise ValueError("measurement output exceeds 1 MiB")
        stdout, stderr = stdout.decode("utf-8"), stderr.decode("utf-8")
        if child.returncode != 0:
            raise subprocess.CalledProcessError(child.returncode, command, stdout, stderr)
        return {"stdout": stdout, "stderr": stderr,
                "seconds": (finished - started) / 1_000_000_000,
                "max_rss_bytes": rss_bytes(usage.ru_maxrss),
                "started_ns": started, "finished_ns": finished}
