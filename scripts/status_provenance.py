"""Prepared POSIX workload identity; content reads occur only before/after samples.

Observations detect changes, not hostile actors restoring files between checks.
This module neither resets caches nor attests that other readers were absent.
"""
import hashlib
import json
import os
from pathlib import Path
import stat


def _identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def _regular(info, owned):
    if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
            or info.st_mode & 0o022 or (owned and info.st_uid != os.geteuid())):
        raise ValueError("provenance requires regular files without links or external writers")


def _read(parent, name, owned):
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
    try:
        before = os.fstat(fd)
        _regular(before, owned)
        digest = hashlib.sha256()
        while True:
            chunk = os.read(fd, 1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
        expected = _identity(before)
        if (expected != _identity(os.fstat(fd))
                or expected != _identity(os.stat(name, dir_fd=parent, follow_symlinks=False))):
            raise ValueError("provenance file changed during hashing")
        return expected, digest.hexdigest()
    finally:
        os.close(fd)


def _anchor(path):
    # Canonicalize ancestors (e.g. macOS /tmp), never the selected leaf symlink.
    path = Path(os.path.abspath(path))
    return path.parent.resolve(strict=True) / path.name


def binary_digest(path):
    """Hash the selected regular binary before receipt preparation."""
    path = _anchor(path)
    parent = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        return _read(parent, path.name, False)[1]
    finally:
        os.close(parent)


class PreparedWorkload:
    """Inventory the whole private synthetic project, including prepared receipts.

    Directory descriptors anchor relative lookups; files are opened one at a time
    so the 10,000-file workload does not require 10,000 open descriptors. atime is
    excluded because legitimate status reads may update it. No fd is inherited.
    """
    def __init__(self, root, binary, expected_binary_sha256):
        self.root, self.binary = _anchor(root), _anchor(binary)
        self.directories, self.files = {}, {}
        self.binary_parent = None
        try:
            root_fd = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            self.directories[""] = (root_fd, None, [])
            info = os.fstat(root_fd)
            self.directories[""] = (root_fd, _identity(info), [])
            if info.st_uid != os.geteuid() or info.st_mode & 0o077:
                raise ValueError("provenance project must be private and owned")
            self._inventory("")
            if not self.files:
                raise ValueError("empty provenance project")
            self.binary_parent = os.open(self.binary.parent,
                                         os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            self.binary_parent_identity = _identity(os.fstat(self.binary_parent))[:2]
            self.binary_identity, digest = _read(self.binary_parent, self.binary.name, False)
            if digest != expected_binary_sha256:
                raise ValueError("binary changed since receipt preparation")
            self.binary_sha256 = digest
            self._manifest = {
                "schema": 1,
                "binary": {"size": self.binary_identity[5], "sha256": digest},
                "files": [{"path": name, "size": record[0][5], "sha256": record[1]}
                          for name, record in sorted(self.files.items())],
            }
            encoded = json.dumps(self._manifest, sort_keys=True, separators=(",", ":"))
            self.manifest_sha256 = hashlib.sha256(encoded.encode()).hexdigest()
            self.validate_metadata()
        except BaseException:
            self.close()
            raise

    def _inventory(self, relative):
        fd, expected, _ = self.directories[relative]
        names = sorted(os.listdir(fd))
        self.directories[relative] = (fd, expected, names)
        for name in names:
            path = relative + "/" + name if relative else name
            info = os.stat(name, dir_fd=fd, follow_symlinks=False)
            if stat.S_ISDIR(info.st_mode):
                child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                self.directories[path] = (child, None, [])
                info = os.fstat(child)
                self.directories[path] = (child, _identity(info), [])
                if info.st_uid != os.geteuid() or info.st_mode & 0o022:
                    raise ValueError("provenance directory has external writers or owner")
                self._inventory(path)
            else:
                self.files[path] = _read(fd, name, True)

    def validate_metadata(self):
        """Check paths/identities without opening or reading file contents."""
        if not self.directories or self.binary_parent is None:
            raise ValueError("provenance snapshot is closed")
        if (_identity(os.stat(self.binary.parent, follow_symlinks=False))[:2]
                != self.binary_parent_identity):
            raise ValueError("binary directory changed")
        if (_identity(os.stat(self.binary.name, dir_fd=self.binary_parent, follow_symlinks=False))
                != self.binary_identity):
            raise ValueError("binary identity changed")
        for relative, (fd, expected, names) in self.directories.items():
            if relative:
                parent, _, name = relative.rpartition("/")
                current = os.stat(name, dir_fd=self.directories[parent][0], follow_symlinks=False)
            else:
                current = os.stat(self.root, follow_symlinks=False)
            if (_identity(current) != expected or _identity(os.fstat(fd)) != expected
                    or sorted(os.listdir(fd)) != names):
                raise ValueError("provenance directory or file set changed")
        for relative, (expected, _) in self.files.items():
            parent, _, name = relative.rpartition("/")
            if _identity(os.stat(name, dir_fd=self.directories[parent][0], follow_symlinks=False)) != expected:
                raise ValueError("provenance file identity changed")

    def verify_contents(self):
        """Rehash outside timed collection; reject the whole run on any change."""
        self.validate_metadata()
        if _read(self.binary_parent, self.binary.name, False) != (self.binary_identity, self.binary_sha256):
            raise ValueError("binary content changed")
        for relative, expected in self.files.items():
            parent, _, name = relative.rpartition("/")
            if _read(self.directories[parent][0], name, True) != expected:
                raise ValueError("provenance file content changed")
        self.validate_metadata()

    def export(self):
        # A caller cannot mutate the expected manifest through output metadata.
        return {"manifest_sha256": self.manifest_sha256,
                "manifest": json.loads(json.dumps(self._manifest))}

    def close(self):
        descriptors = [record[0] for record in self.directories.values()]
        self.directories.clear()
        if self.binary_parent is not None:
            descriptors.append(self.binary_parent)
            self.binary_parent = None
        failure = None
        for fd in descriptors:
            try:
                os.close(fd)
            except OSError as error:
                failure = failure or error
        if failure is not None:
            raise failure

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        self.close()
