"""Verify the release archive allowlist, checksums and refusal paths."""

import hashlib
import importlib.util
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
import zipfile

SCRIPT = Path(__file__).with_name("package-release.py")
SPEC = importlib.util.spec_from_file_location("package_release", SCRIPT)
PACKAGING = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PACKAGING)
REVISION = "c" * 40
VERSION = "v1.2.3"
BINARIES = (
    "naru-darwin-amd64", "naru-darwin-arm64", "naru-linux-amd64",
    "naru-linux-arm64", "naru-windows-amd64.exe",
)
METADATA = ("LICENSE", "REVISION", "VERSION", "TOOLCHAIN", "BUILD_SETTINGS")
ARCHIVES = (
    "naru_1.2.3_darwin_amd64.tar.gz", "naru_1.2.3_darwin_arm64.tar.gz",
    "naru_1.2.3_linux_amd64.tar.gz", "naru_1.2.3_linux_arm64.tar.gz",
    "naru_1.2.3_windows_amd64.zip",
)


class ReleasePackaging(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.candidate = self.root / "candidate"
        self.candidate.mkdir()
        self.output = self.root / "release"
        for name in (*BINARIES, *METADATA):
            (self.candidate / name).write_text(f"{name}\n", encoding="ascii")
        (self.candidate / "REVISION").write_text(REVISION + "\n", encoding="ascii")
        (self.candidate / "VERSION").write_text(VERSION + "\n", encoding="ascii")
        self.manifest = self.candidate / "SHA256SUMS"
        self.manifest.write_text(self.sums(self.candidate, (*BINARIES, *METADATA)), encoding="ascii")

    @staticmethod
    def sums(directory, names):
        return "".join(
            hashlib.sha256((directory / name).read_bytes()).hexdigest() + f"  {name}\n"
            for name in sorted(names)
        )

    def package(self):
        PACKAGING.package(self.candidate, self.output, REVISION, VERSION)

    def test_exact_archives_members_and_checksums(self):
        self.package()
        self.assertEqual({p.name for p in self.output.iterdir()}, {"SHA256SUMS", *ARCHIVES})
        self.assertEqual((self.output / "SHA256SUMS").read_text(), self.sums(self.output, ARCHIVES))
        for name, binary in zip(ARCHIVES, BINARIES):
            archive = self.output / name
            if name.endswith(".zip"):
                with zipfile.ZipFile(archive) as bundle:
                    self.assertEqual(sorted(bundle.namelist()), sorted((*METADATA, "naru.exe")))
                    self.assertEqual(bundle.read("naru.exe"), (self.candidate / binary).read_bytes())
                    for member in METADATA:
                        self.assertEqual(bundle.read(member), (self.candidate / member).read_bytes())
            else:
                with tarfile.open(archive) as bundle:
                    self.assertEqual(sorted(bundle.getnames()), sorted((*METADATA, "naru")))
                    self.assertTrue(all(member.isfile() for member in bundle))
                    self.assertEqual(bundle.extractfile("naru").read(), (self.candidate / binary).read_bytes())
                    for member in METADATA:
                        self.assertEqual(bundle.extractfile(member).read(), (self.candidate / member).read_bytes())

    def test_unknown_entry_is_rejected(self):
        (self.candidate / ".private").write_text("unexpected input")
        with self.assertRaisesRegex(ValueError, "exactly the expected"):
            self.package()
        self.assertFalse(self.output.exists())

    def test_symlink_is_rejected(self):
        candidate_binary = self.candidate / BINARIES[0]
        moved = self.root / BINARIES[0]
        candidate_binary.rename(moved)
        candidate_binary.symlink_to(moved)
        with self.assertRaisesRegex(ValueError, "regular file"):
            self.package()
        self.assertFalse(self.output.exists())

    def test_manifest_cannot_name_paths_outside_candidate(self):
        with self.manifest.open("a", encoding="ascii") as manifest:
            manifest.write("a" * 64 + "  ../outside\n")
        with self.assertRaisesRegex(ValueError, "checksum manifest"):
            self.package()
        self.assertFalse(self.output.exists())

    def test_existing_output_is_preserved(self):
        self.output.mkdir()
        sentinel = self.output / "existing"
        sentinel.write_bytes(b"preserve me")
        with self.assertRaises(FileExistsError):
            self.package()
        self.assertEqual(list(self.output.iterdir()), [sentinel])
        self.assertEqual(sentinel.read_bytes(), b"preserve me")

    def test_revision_mismatch_is_rejected(self):
        (self.candidate / "REVISION").write_text("d" * 40 + "\n")
        with self.assertRaisesRegex(ValueError, "revision changed"):
            self.package()
        self.assertFalse(self.output.exists())

    def test_cli_refuses_parent_traversal(self):
        result = subprocess.run(
            [sys.executable, str(SCRIPT), str(self.root / ".." / "release"), VERSION],
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(result.returncode, 2)
        self.assertIn("without parent traversal", result.stderr)


if __name__ == "__main__":
    unittest.main()
