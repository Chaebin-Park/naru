"""Exercise prepared workload provenance without cache resets or privileges."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import status_provenance as provenance


@unittest.skipUnless(os.name == "posix", "requires POSIX no-follow and directory descriptors")
class ProvenanceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.root = self.base / "project"
        self.root.mkdir(mode=0o700)
        (self.root / "src").mkdir()
        (self.root / ".naru/state").mkdir(parents=True)
        for name, data in {"src/a": b"aaaa", "src/b": b"bbbb",
                           ".naru/config.yaml": b"synthetic config",
                           ".naru/state/receipt.json": b"synthetic receipt"}.items():
            (self.root / name).write_bytes(data)
        self.binary = self.base / "naru"
        self.binary.write_bytes(b"synthetic binary")
        self.digest = provenance.binary_digest(self.binary)

    def snapshot(self):
        return provenance.PreparedWorkload(self.root, self.binary, self.digest)

    def test_manifest_and_metadata_only_validation(self):
        with self.snapshot() as prepared:
            with patch.object(provenance.os, "read", side_effect=AssertionError("content read")), \
                    patch.object(provenance.os, "open", side_effect=AssertionError("file open")):
                prepared.validate_metadata()
            # A normal read may update atime, but must not invalidate the snapshot.
            (self.root / "src/a").read_bytes()
            prepared.verify_contents()
            output = prepared.export()
            manifest = output["manifest"]
            encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
            self.assertEqual(output["manifest_sha256"], hashlib.sha256(encoded).hexdigest())
            self.assertEqual(manifest["binary"]["sha256"], self.digest)
            self.assertEqual({row["path"] for row in manifest["files"]},
                             {"src/a", "src/b", ".naru/config.yaml", ".naru/state/receipt.json"})
            self.assertTrue(all(row["sha256"] == hashlib.sha256(
                (self.root / row["path"]).read_bytes()).hexdigest() for row in manifest["files"]))
            output["manifest"]["files"].clear()
            self.assertEqual(len(prepared.export()["manifest"]["files"]), 4)

    def test_reject_content_changes_for_every_role(self):
        for path in [self.binary, self.root / "src/a", self.root / ".naru/config.yaml",
                     self.root / ".naru/state/receipt.json"]:
            with self.subTest(path=path.name):
                original = path.read_bytes()
                with self.snapshot() as prepared:
                    path.write_bytes(b"x" * len(original))
                    with self.assertRaises(ValueError):
                        prepared.validate_metadata()
                    with self.assertRaises(ValueError):
                        prepared.verify_contents()
                path.write_bytes(original)

    def test_post_sample_hash_checks_bytes_even_if_metadata_is_uninformative(self):
        target = self.root / "src/a"
        before = target.stat()
        original_identity = provenance._identity
        with self.snapshot() as prepared:
            target.write_bytes(b"zzzz")
            def frozen_target(info):
                return original_identity(before if info.st_ino == before.st_ino else info)
            with patch.object(provenance, "_identity", side_effect=frozen_target):
                prepared.validate_metadata()
                with self.assertRaisesRegex(ValueError, "content changed"):
                    prepared.verify_contents()

    def test_reject_file_addition_removal_and_same_content_replacement(self):
        target = self.root / "src/a"
        for change in ["add", "remove", "replace", "symlink"]:
            with self.subTest(change=change), self.snapshot() as prepared:
                if change == "add":
                    (self.root / "src/extra").write_bytes(b"new")
                else:
                    replacement = self.base / "replacement"
                    replacement.write_bytes(target.read_bytes())
                    target.unlink()
                    if change == "replace":
                        replacement.rename(target)
                    elif change == "symlink":
                        target.symlink_to(replacement)
                with self.assertRaises((ValueError, OSError)):
                    prepared.verify_contents()
            if target.is_symlink():
                target.unlink()
            target.write_bytes(b"aaaa")
            (self.root / "src/extra").unlink(missing_ok=True)

    def test_reject_root_directory_and_binary_replacement(self):
        for target in [self.root, self.root / "src", self.binary]:
            with self.subTest(target=target.name), self.snapshot() as prepared:
                moved = target.with_name(target.name + "-old")
                target.rename(moved)
                if moved.is_dir():
                    target.mkdir(mode=0o700)
                else:
                    target.write_bytes(moved.read_bytes())
                with self.assertRaises(ValueError):
                    prepared.validate_metadata()
                if target.is_dir():
                    target.rmdir()
                else:
                    target.unlink()
                moved.rename(target)

    def test_reject_unsafe_files_and_directories_at_preparation(self):
        extra = self.root / "src/extra"
        for kind in ["symlink", "directory-symlink", "hardlink", "fifo", "writable"]:
            with self.subTest(kind=kind):
                if kind == "symlink": extra.symlink_to(self.binary)
                elif kind == "directory-symlink": extra.symlink_to(self.base, target_is_directory=True)
                elif kind == "hardlink": os.link(self.root / "src/a", extra)
                elif kind == "fifo": os.mkfifo(extra)
                else:
                    extra.write_bytes(b"x")
                    extra.chmod(0o666)
                with self.assertRaises((ValueError, OSError)):
                    self.snapshot()
                extra.unlink()
        self.root.chmod(0o755)
        with self.assertRaises(ValueError): self.snapshot()
        self.root.chmod(0o700)
        root_link = self.base / "project-link"
        root_link.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(OSError):
            provenance.PreparedWorkload(root_link, self.binary, self.digest)
        (self.root / "src").chmod(0o777)
        with self.assertRaises(ValueError): self.snapshot()
        (self.root / "src").chmod(0o755)
        link = self.base / "binary-link"
        link.symlink_to(self.binary)
        with self.assertRaises(OSError): provenance.binary_digest(link)
        with self.assertRaises(OSError):
            provenance.PreparedWorkload(self.root, link, self.digest)
        with self.assertRaisesRegex(ValueError, "binary changed"):
            provenance.PreparedWorkload(self.root, self.binary, "0" * 64)

    def test_cleanup_on_success_and_failed_preparation(self):
        original_open = os.open
        opened = []
        def tracked_open(*args, **kwargs):
            fd = original_open(*args, **kwargs)
            opened.append(fd)
            return fd
        with patch.object(provenance.os, "open", side_effect=tracked_open):
            with self.snapshot(): pass
            (self.root / "unsafe").symlink_to(self.binary)
            with self.assertRaises(OSError): self.snapshot()
        for fd in set(opened):
            with self.assertRaises(OSError): os.fstat(fd)

    def test_constructor_stat_error_closes_opened_directory(self):
        original_open = os.open
        opened = []
        def tracked_open(*args, **kwargs):
            fd = original_open(*args, **kwargs)
            opened.append(fd)
            return fd
        with patch.object(provenance.os, "open", side_effect=tracked_open), \
                patch.object(provenance.os, "fstat", side_effect=OSError("injected stat failure")):
            with self.assertRaises(OSError): self.snapshot()
        self.assertTrue(opened)
        for fd in set(opened):
            with self.assertRaises(OSError): os.fstat(fd)

    def test_many_files_with_small_descriptor_budget(self):
        for number in range(300):
            (self.root / "src" / str(number)).write_bytes(b"x")
        original_open, original_close = os.open, os.close
        active = set()
        def bounded_open(*args, **kwargs):
            if len(active) >= 16:
                raise OSError("descriptor budget exhausted")
            fd = original_open(*args, **kwargs)
            active.add(fd)
            self.assertFalse(os.get_inheritable(fd))
            return fd
        def tracked_close(fd):
            original_close(fd)
            active.discard(fd)
        with patch.object(provenance.os, "open", side_effect=bounded_open), \
                patch.object(provenance.os, "close", side_effect=tracked_close):
            with self.snapshot() as prepared:
                prepared.validate_metadata()
                prepared.verify_contents()
        self.assertFalse(active)

    def test_measurement_rejects_mutation_despite_successful_status(self):
        script = Path(__file__).with_name("measure-status.py")
        for change in ["input", "receipt", "binary-during-preparation"]:
            with self.subTest(change=change):
                prepare = "mkdir -p .naru/state; printf receipt > .naru/state/receipt.json"
                mutate = {"input": "printf b > src/00000.txt",
                          "receipt": "printf changed > .naru/state/receipt.json",
                          "binary-during-preparation": ":"}[change]
                if change == "binary-during-preparation":
                    prepare += '; printf "\\n# changed\\n" >> "$0"'
                self.binary.write_text('#!/bin/sh\ncase "$1" in\ncheck) ' + prepare
                    + ';;\nstatus) ' + mutate + '; printf \'%s\\n\' \'{"satisfied":true,'
                    '"checks":[{"check":"check0","state":"pass"}]}\';;\nesac\n')
                self.binary.chmod(0o700)
                run = subprocess.run([sys.executable, "-B", str(script), str(self.binary),
                    "--cache-mode", "first-run", "--files", "1", "--bytes", "1"],
                    capture_output=True, text=True, timeout=20)
                self.assertNotEqual(run.returncode, 0)
                self.assertEqual(run.stdout, "")
                self.assertIn("changed", run.stderr)


if __name__ == "__main__":
    unittest.main()
