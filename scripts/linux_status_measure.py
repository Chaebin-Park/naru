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


def collect(command, cwd, timeout=120):
    if platform.system() != "Linux" or not hasattr(os, "wait4"):
        raise RuntimeError("Linux wait4 collection requires Linux")
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError("measurement timeout must be positive")
    # Files avoid pipe deadlock while this function exclusively owns wait4/reaping.
    with tempfile.TemporaryFile() as output, tempfile.TemporaryFile() as errors:
        started = time.monotonic_ns()
        child = subprocess.Popen(command, cwd=cwd, stdin=subprocess.DEVNULL,
                                 stdout=output, stderr=errors, start_new_session=True)
        try:
            while True:
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
            _, status, _ = os.wait4(child.pid, 0)
            child.returncode = os.waitstatus_to_exitcode(status)
            raise
        finished = time.monotonic_ns()
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
