"""Check labels, rejection and summary math without manipulating host caches."""
import contextlib
import importlib.util
import io
import platform
import signal
import subprocess
import sys
import tempfile
from unittest.mock import Mock, patch
import linux_status_measure as linux
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location(
    "measure_status", Path(__file__).with_name("measure-status.py"))
measure = importlib.util.module_from_spec(spec)
spec.loader.exec_module(measure)


class MeasurementTests(unittest.TestCase):
    def test_summary(self):
        rows = [{"seconds": n, "max_rss_bytes": 100 - n} for n in range(1, 21)]
        self.assertEqual(measure.summarize(rows), {
            "median_seconds": 10.5, "p95_seconds": 19, "max_rss_bytes": 99})
        self.assertEqual(measure.summarize(rows[:1])["p95_seconds"], 1)
        with self.assertRaises(ValueError):
            measure.summarize([])

    def test_labels_and_defaults(self):
        warm = measure.parse_args(["naru"])
        self.assertEqual((warm.cache_mode, warm.samples, warm.checks), ("warm-cache", 20, 1))
        first = measure.parse_args(["naru", "--cache-mode", "first-run"])
        self.assertEqual((first.cache_mode, first.samples), ("first-run", 1))

    def test_reader_guard_platform_and_unavailable_setup(self):
        with patch.object(measure.platform, "system", return_value="Darwin"), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                measure.parse_args(["naru", "--reader-guard"])
        with patch.object(measure.platform, "system", return_value="Linux"):
            self.assertTrue(measure.parse_args(["naru", "--reader-guard"]).reader_guard)
        args = measure.parse_args(["naru", "--files", "1", "--bytes", "1"])
        args.reader_guard = True
        output = io.StringIO()
        with patch.object(measure, "parse_args", return_value=args), patch.object(measure.platform, "system", return_value="Linux"), patch.object(measure, "binary_digest", return_value="sha"), patch.object(measure.subprocess, "run"), patch.object(measure, "PreparedWorkload"), patch.object(measure, "ReaderGuard", side_effect=PermissionError("unavailable")), patch.object(measure, "collect_linux") as collect, contextlib.redirect_stdout(output):
            with self.assertRaises(PermissionError): measure.main()
            collect.assert_not_called()
        self.assertEqual(output.getvalue(), "")

    def test_reject_invalid_and_unproven_cold(self):
        for flags in (["--samples", "0"], ["--samples", "-1"],
                      ["--checks", "0"], ["--files", "0"], ["--bytes", "0"],
                      ["--cache-mode", "first-run", "--samples", "2"],
                      ["--cache-mode", "cold-cache"]):
            with self.subTest(flags=flags), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    measure.parse_args(["naru", *flags])
                self.assertEqual(error.exception.code, 2)


class LinuxCollectorTests(unittest.TestCase):
    def test_rss_units_and_rejections(self):
        self.assertEqual(linux.rss_bytes(7), 7168)
        for invalid in [0, -1, True, 1.5, None]:
            with self.subTest(value=invalid), self.assertRaises(ValueError):
                linux.rss_bytes(invalid)
        with patch.object(linux.platform, "system", return_value="Windows"):
            with self.assertRaises(RuntimeError): linux.collect([], ".")

    def test_lost_reaping_ownership_rejects_without_signaling(self):
        child = Mock(pid=123, returncode=None)
        with patch.object(linux.platform, "system", return_value="Linux"), patch.object(linux.subprocess, "Popen", return_value=child), patch.object(linux.os, "wait4", create=True, side_effect=ChildProcessError()), patch.object(linux.os, "WNOHANG", create=True, new=1), patch.object(linux.os, "killpg", create=True) as kill:
            with self.assertRaises(RuntimeError): linux.collect(["unused"], ".")
            kill.assert_not_called()
        self.assertEqual(child.returncode, 255)

    def test_status_rejects_false_success(self):
        valid = {"satisfied": True, "checks": [{"check": "check0", "state": "pass"}]}
        measure.validate_status(valid, ["check0"])
        for bad in [{}, {**valid, "satisfied": "true"}, {**valid, "checks": []},
                    {**valid, "checks": [{"check": "wrong", "state": "pass"}]},
                    {**valid, "checks": [{"check": "check0", "state": "stale"}]},
                    {**valid, "checks": [None]}]:
            with self.subTest(report=bad), self.assertRaises(RuntimeError):
                measure.validate_status(bad, ["check0"])

    @unittest.skipUnless(platform.system() == "Linux", "requires real Linux wait4")
    def test_real_child_exit_signal_timeout_and_output(self):
        with tempfile.TemporaryDirectory() as root:
            result = linux.collect([sys.executable, "-c", "print('ok')"], root)
            self.assertEqual(result["stdout"], "ok\n")
            self.assertGreater(result["max_rss_bytes"], 0)
            self.assertGreater(result["finished_ns"], result["started_ns"])
            for code, expected in [("raise SystemExit(7)", 7),
                                   ("import os,signal;os.kill(os.getpid(),signal.SIGTERM)", -signal.SIGTERM)]:
                with self.subTest(code=code), self.assertRaises(subprocess.CalledProcessError) as failure:
                    linux.collect([sys.executable, "-c", code], root)
                self.assertEqual(failure.exception.returncode, expected)
            with self.assertRaises(subprocess.TimeoutExpired):
                linux.collect([sys.executable, "-c", "import time;time.sleep(60)"], root, timeout=.05)
            with self.assertRaises(ValueError):
                linux.collect([sys.executable, "-c", "print('x'*1048577)"], root)
            with self.assertRaises(FileNotFoundError):
                linux.collect([str(Path(root)/"missing")], root)


if __name__ == "__main__":
    unittest.main()
