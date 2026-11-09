"""Check labels, rejection and summary math without manipulating host caches."""
import contextlib
import importlib.util
import io
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

    def test_reject_invalid_and_unproven_cold(self):
        for flags in (["--samples", "0"], ["--samples", "-1"],
                      ["--checks", "0"], ["--files", "0"], ["--bytes", "0"],
                      ["--cache-mode", "first-run", "--samples", "2"],
                      ["--cache-mode", "cold-cache"]):
            with self.subTest(flags=flags), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as error:
                    measure.parse_args(["naru", *flags])
                self.assertEqual(error.exception.code, 2)


if __name__ == "__main__":
    unittest.main()
