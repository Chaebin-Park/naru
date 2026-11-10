"""Portable rejection tests. Injected probes are not Linux runtime evidence."""
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
import linux_cache_probe as cache


class ProbeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.file = self.root / "input"
        self.file.write_bytes(b"a" * (os.sysconf("SC_PAGE_SIZE") + 1))
        self.file.chmod(0o600)

    def test_zero_partial_and_invalid(self):
        with cache.Fixture(self.root) as fixture:
            zero = fixture.snapshot(lambda fd, size: (2, 0))
            cache.require_nonresident(zero)
            with self.assertRaises(ValueError):
                cache.require_nonresident(fixture.snapshot(lambda fd, size: (2, 1)))
            for result in [(0, 0), (2, 3), (2, -1), (True, 0)]:
                with self.subTest(result=result), self.assertRaises(ValueError):
                    fixture.snapshot(lambda fd, size: result)
            with self.assertRaises(OSError):
                fixture.snapshot(lambda fd, size: (_ for _ in ()).throw(OSError("probe unavailable")))

    def test_changes_rejected(self):
        for change in ["replace", "write", "extra", "symlink"]:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                target = root / "input"
                target.write_bytes(b"a")
                with cache.Fixture(root) as fixture:
                    if change == "replace":
                        target.unlink()
                        target.write_bytes(b"a")
                    elif change == "write":
                        target.write_bytes(b"bb")
                    elif change == "extra":
                        (root / "extra").write_bytes(b"a")
                    else:
                        target.unlink()
                        target.symlink_to(self.file)
                    with self.assertRaises(ValueError):
                        fixture.snapshot(lambda fd, size: (1, 0))

    def test_unsafe_inputs(self):
        self.file.unlink()
        for kind in ["empty", "symlink", "directory", "hardlink"]:
            with self.subTest(kind=kind):
                if kind == "empty": self.file.touch()
                elif kind == "symlink": self.file.symlink_to("/etc/passwd")
                elif kind == "directory": self.file.mkdir()
                else:
                    other = self.root / "other"
                    other.write_bytes(b"a")
                    os.link(other, self.file)
                with self.assertRaises((OSError, ValueError)):
                    cache.Fixture(self.root)
                if kind == "directory": self.file.rmdir()
                else: self.file.unlink()
                if kind == "hardlink": other.unlink()
        self.file.write_bytes(b"a")
        with patch.object(cache.os, "geteuid", return_value=os.geteuid() + 1):
            with self.assertRaises(ValueError): cache.Fixture(self.root)

    def test_syscall_failures(self):
        for stage in ["mmap", "mincore", "munmap"]:
            with self.subTest(stage=stage):
                libc = Mock()
                libc.mmap.return_value = 4096
                libc.mincore.return_value = 0
                libc.munmap.return_value = 0
                if stage == "mmap":
                    libc.mmap.return_value = cache.ctypes.c_void_p(-1).value
                else:
                    getattr(libc, stage).return_value = -1
                with patch.object(cache.platform, "system", return_value="Linux"), patch.object(cache.ctypes, "CDLL", return_value=libc):
                    with self.assertRaises(OSError): cache.resident_pages(0, 1)
                self.assertEqual(libc.munmap.call_count, 0 if stage == "mmap" else 1)

    def test_empty_and_writable_directory(self):
        self.file.unlink()
        with self.assertRaises(ValueError): cache.Fixture(self.root)
        self.file.write_bytes(b"a")
        self.root.chmod(0o755)
        with self.assertRaises(ValueError): cache.Fixture(self.root)
        self.root.chmod(0o700)
        self.file.chmod(0o666)
        with self.assertRaises(ValueError): cache.Fixture(self.root)

    def test_mutation_during_probe_and_unsupported(self):
        with cache.Fixture(self.root) as fixture:
            def mutate(fd, size):
                self.file.write_bytes(b"changed")
                return 2, 0
            with self.assertRaises(ValueError): fixture.snapshot(mutate)
        with patch.object(cache.platform, "system", return_value="Darwin"):
            with self.assertRaises(RuntimeError): cache.resident_pages(0, 1)


if __name__ == "__main__":
    unittest.main()
