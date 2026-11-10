"""Opt-in fanotify permission gate for a prepared, isolated synthetic fixture.

Default fanotify PID reporting is TGID (not FAN_REPORT_TID). This is a measurement
validity check, not a sandbox; closing a failed group can allow pending opens.
"""
import ctypes
import os
import platform
from pathlib import Path
import struct

_HEADER = struct.Struct('=IBBHQii')
_PERMISSION = 0x10000 | 0x20000 | 0x40000  # OPEN, ACCESS, OPEN_EXEC
_CHILD = 0x08000000
_OVERFLOW = 0x4000


def assert_no_existing_readers(identities, proc=Path('/proc'), probe_fds=()):
    """Fail closed on visible pre-existing regular file descriptors or mappings.

    Procfs visibility is required; this is a bounded observation of an isolated
    environment, not an atomic proof against racing privileged processes.
    """
    probe_fds = frozenset(probe_fds)
    if os.geteuid() != 0:
        raise RuntimeError('reader guard requires root and complete procfs visibility')
    mounts = (proc / 'mounts').read_text()
    if not any(line.split()[1:3] == ['/proc', 'proc'] for line in mounts.splitlines()):
        raise RuntimeError('reader guard requires mounted procfs')
    if 'hidepid=' in mounts:
        raise RuntimeError('reader guard refuses restricted procfs visibility')
    for process in proc.iterdir():
        if not process.name.isdecimal():
            continue
        try:
            for entry in (process / 'fd').iterdir():
                try:
                    info = entry.stat()
                except FileNotFoundError:  # A descriptor closed while enumerating.
                    continue
                if (info.st_dev, info.st_ino) in identities:
                    # Only this controller's exact, non-inherited read-only probe
                    # descriptors may predate reset. Never exempt another process
                    # or any mapping; the probe must unmap and close before launch.
                    if process.name == str(os.getpid()) and int(entry.name) in probe_fds:
                        continue
                    raise RuntimeError('reader guard found an existing file descriptor')
            for line in (process / 'maps').read_text().splitlines():
                fields = line.split(None, 5)
                if len(fields) < 5:
                    raise RuntimeError('reader guard encountered malformed procfs maps')
                major, minor = fields[3].split(':')
                identity = (os.makedev(int(major, 16), int(minor, 16)), int(fields[4]))
                if identity in identities:
                    raise RuntimeError('reader guard found an existing file mapping')
        except (FileNotFoundError, ProcessLookupError):
            if process.exists():
                raise RuntimeError('reader guard could not inspect a live process')
        # Permission and other inspection failures deliberately propagate.


class ReaderGuard:
    """Single-owner synchronous monitor; no thread or monitor process can die unseen."""
    def __init__(self, prepared, probe_fds=()):
        self.fd = None
        self.failed = False
        self.allowed_events = 0
        if platform.system() != 'Linux':
            raise RuntimeError('reader guard requires Linux fanotify')
        prepared.validate_metadata()
        libc = ctypes.CDLL(None, use_errno=True)
        libc.fanotify_init.argtypes = [ctypes.c_uint, ctypes.c_uint]
        libc.fanotify_init.restype = ctypes.c_int
        libc.fanotify_mark.argtypes = [ctypes.c_int, ctypes.c_uint, ctypes.c_uint64,
                                      ctypes.c_int, ctypes.c_char_p]
        libc.fanotify_mark.restype = ctypes.c_int
        fd = libc.fanotify_init(0x1 | 0x2 | 0x4, os.O_RDONLY | os.O_CLOEXEC)
        if fd < 0:
            raise OSError(ctypes.get_errno(), 'reader guard fanotify_init')
        self.fd = fd
        try:
            # Directory marks cover existing direct children without one mark/FD per
            # input. Provenance rejects additions, renames and directory replacement.
            for directory_fd, _, _ in prepared.directories.values():
                path = f'/proc/self/fd/{directory_fd}'.encode()
                if libc.fanotify_mark(fd, 0x1 | 0x8, _PERMISSION | _CHILD, -100, path):
                    raise OSError(ctypes.get_errno(), 'reader guard directory mark')
            if libc.fanotify_mark(fd, 0x1 | 0x4, _PERMISSION, prepared.binary_parent,
                                 os.fsencode(prepared.binary.name)):
                raise OSError(ctypes.get_errno(), 'reader guard binary mark')
            identities = {record[0][:2] for record in prepared.files.values()}
            identities.add(prepared.binary_identity[:2])
            if probe_fds:
                input_identities = {r[0][:2] for name, r in prepared.files.items() if name.startswith('src/')}
                import fcntl
                for probe_fd in probe_fds:
                    info = os.fstat(probe_fd)
                    if ((info.st_dev, info.st_ino) not in input_identities
                            or os.get_inheritable(probe_fd)
                            or fcntl.fcntl(probe_fd, fcntl.F_GETFL) & os.O_ACCMODE != os.O_RDONLY):
                        raise ValueError('reader guard requires exact non-inherited read-only fixture probe descriptors')
            assert_no_existing_readers(identities, probe_fds=probe_fds)
            prepared.validate_metadata()
            self.service(None)  # No PID is authorized before launch.
        except BaseException:
            self.close()
            raise

    def _respond(self, data, selected_pid):
        events, descriptors = [], []
        try:
            offset = 0
            while offset < len(data):
                if len(data) - offset < _HEADER.size:
                    raise RuntimeError('reader guard truncated event')
                event = _HEADER.unpack_from(data, offset)
                length, _, _, _, _, fd, _ = event
                if fd >= 0:
                    descriptors.append(fd)
                events.append(event)
                if length < _HEADER.size or length > len(data) - offset:
                    raise RuntimeError('reader guard invalid event length')
                offset += length
            # Parse the entire batch first so later descriptors close on rejection.
            for length, version, _, metadata, mask, fd, pid in events:
                if (length != _HEADER.size or version != 3 or metadata != _HEADER.size
                        or mask & _OVERFLOW or mask == 0 or mask & ~_PERMISSION or fd < 0):
                    raise RuntimeError('reader guard unsupported event or queue overflow')
                allowed = selected_pid is not None and pid == selected_pid
                response = struct.pack('=iI', fd, 1 if allowed else 2)
                if os.write(self.fd, response) != len(response):
                    raise RuntimeError('reader guard incomplete permission response')
                if not allowed:
                    raise RuntimeError('reader guard rejected an unexpected reader')
                self.allowed_events += 1
        finally:
            failure = None
            for fd in descriptors:
                try:
                    os.close(fd)
                except OSError as error:
                    failure = failure or error
            if failure:
                raise failure

    def service(self, selected_pid):
        if self.fd is None or self.failed:
            raise RuntimeError('reader guard is closed or failed')
        try:
            for _ in range(256):
                try:
                    data = os.read(self.fd, 65536)
                except BlockingIOError:
                    return
                if not data:
                    raise RuntimeError('reader guard monitor EOF')
                self._respond(data, selected_pid)
            raise RuntimeError('reader guard event queue did not drain')
        except BaseException:
            self.failed = True
            raise

    def close(self):
        if self.fd is not None:
            fd, self.fd = self.fd, None
            os.close(fd)

    def __enter__(self):
        return self

    def __exit__(self, *unused):
        self.close()
