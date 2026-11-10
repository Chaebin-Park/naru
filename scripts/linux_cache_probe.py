"""Read-only Linux file-page residency snapshots; never resets OS caches.

Callers must isolate the guest and prepare/open fixtures before resetting caches.
A zero snapshot is not a cold-performance attestation or a race-free guarantee.
"""
import ctypes
import os
import platform
import stat
import time


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def resident_pages(fd, size):
    if platform.system() != "Linux":
        raise RuntimeError("mincore probe requires Linux")
    if size <= 0:
        raise ValueError("probe requires nonempty files")
    libc = ctypes.CDLL(None, use_errno=True)
    libc.mmap.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_int,
                         ctypes.c_int, ctypes.c_int, ctypes.c_longlong]
    libc.mmap.restype = ctypes.c_void_p
    libc.mincore.argtypes = [ctypes.c_void_p, ctypes.c_size_t,
                            ctypes.POINTER(ctypes.c_ubyte)]
    libc.mincore.restype = ctypes.c_int
    libc.munmap.argtypes = [ctypes.c_void_p, ctypes.c_size_t]
    libc.munmap.restype = ctypes.c_int
    pages = (size + os.sysconf("SC_PAGE_SIZE") - 1) // os.sysconf("SC_PAGE_SIZE")
    # PROT_READ, MAP_SHARED: no MAP_POPULATE and no access to mapped bytes.
    address = libc.mmap(None, size, 1, 1, fd, 0)
    if address == ctypes.c_void_p(-1).value:
        raise OSError(ctypes.get_errno(), "mmap failed")
    try:
        vector = (ctypes.c_ubyte * pages)()
        if libc.mincore(address, size, vector) != 0:
            raise OSError(ctypes.get_errno(), "mincore failed")
        return pages, sum(value & 1 for value in vector)
    finally:
        if libc.munmap(address, size) != 0:
            raise OSError(ctypes.get_errno(), "munmap failed")


class Fixture:
    """Pin a private, flat directory and its owned regular files before reset.

    Hash/content provenance belongs to the preparing controller. Reject concurrent
    identity changes; same-user hostile writes and snapshot races remain outside
    this observational helper's guarantees.
    """
    def __init__(self, root):
        self.root_fd = None
        self.files = {}
        try:
            self.root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            info = os.fstat(self.root_fd)
            if info.st_uid != os.geteuid() or info.st_mode & 0o077:
                raise ValueError("fixture directory must be private and owned")
            self.root_identity = (info.st_dev, info.st_ino)
            names = sorted(os.listdir(self.root_fd))
            if not names:
                raise ValueError("empty fixture")
            for name in names:
                fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                             dir_fd=self.root_fd)
                self.files[name] = (fd, identity(os.fstat(fd)))
                self._validate_file(os.fstat(fd))
        except BaseException:
            self.close()
            raise

    @staticmethod
    def _validate_file(info):
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid()
                or info.st_nlink != 1 or info.st_size <= 0 or info.st_mode & 0o022):
            raise ValueError("fixture requires owned nonempty unlinked regular files without external writers")

    def validate(self):
        if sorted(os.listdir(self.root_fd)) != sorted(self.files):
            raise ValueError("fixture file set changed")
        for name, (fd, expected) in self.files.items():
            current = os.stat(name, dir_fd=self.root_fd, follow_symlinks=False)
            if identity(current) != expected or identity(os.fstat(fd)) != expected:
                raise ValueError("fixture identity changed")

    def snapshot(self, probe=resident_pages):
        self.validate()
        started = time.monotonic_ns()
        pages = resident = 0
        for fd, expected in self.files.values():
            count, cached = probe(fd, expected[5])
            expected_pages = (expected[5] + os.sysconf("SC_PAGE_SIZE") - 1) // os.sysconf("SC_PAGE_SIZE")
            if (type(count) is not int or type(cached) is not int
                    or count != expected_pages or not 0 <= cached <= count):
                raise ValueError("invalid residency result")
            pages += count
            resident += cached
        self.validate()
        return {"scope": "guest-input-file-page-cache-snapshot", "files": len(self.files),
                "pages": pages, "resident_pages": resident,
                "started_ns": started, "finished_ns": time.monotonic_ns()}

    def close(self):
        for fd, _ in self.files.values():
            os.close(fd)
        self.files.clear()
        if self.root_fd is not None:
            os.close(self.root_fd)
            self.root_fd = None

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        self.close()


def require_nonresident(snapshot):
    if (snapshot["scope"] != "guest-input-file-page-cache-snapshot"
            or type(snapshot["pages"]) is not int or snapshot["pages"] <= 0
            or type(snapshot["resident_pages"]) is not int or snapshot["resident_pages"] != 0):
        raise ValueError("input pages are resident or snapshot is invalid")
