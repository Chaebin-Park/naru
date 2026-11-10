"""Portable failure protocol tests; --native additionally requires real Linux/root."""
import json
import ctypes
import os
from pathlib import Path
import platform
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

import linux_reader_guard as reader
import linux_status_measure as collector
from status_provenance import PreparedWorkload, binary_digest

NATIVE = '--native' in sys.argv
if NATIVE:
    sys.argv.remove('--native')
    if platform.system() != 'Linux' or os.geteuid() != 0:
        raise SystemExit('--native requires Linux root; never skip required kernel cases')


def event(fd, pid=123, mask=0x10000, version=3):
    return struct.pack('=IBBHQii', 24, version, 0, 24, mask, fd, pid)


def fake_guard():
    guard = reader.ReaderGuard.__new__(reader.ReaderGuard)
    guard.fd, guard.failed, guard.allowed_events = 99, False, 0
    return guard


class ProtocolTests(unittest.TestCase):
    def test_tgid_response_and_every_descriptor_closed(self):
        for pid in [123, 124, None]:
            guard = fake_guard()
            with patch.object(reader.os, 'write', return_value=8) as write, patch.object(reader.os, 'close') as close:
                if pid == 123:
                    guard._respond(event(10) + event(11), pid)
                    self.assertEqual(guard.allowed_events, 2)
                else:
                    with self.assertRaisesRegex(RuntimeError, 'unexpected reader'):
                        guard._respond(event(10) + event(11), pid)
                self.assertEqual(write.call_args_list[0].args, (99, struct.pack('=iI', 10, 1 if pid == 123 else 2)))
                self.assertEqual([c.args[0] for c in close.call_args_list], [10, 11])

    def test_bad_batches_responses_and_sticky_failures(self):
        for data in [b'', b'x', event(10, version=2) + event(11),
                     event(-1, mask=0x4000), event(10, mask=0), event(10, mask=1),
                     struct.pack('=IBBHQii', 25, 3, 0, 24, 0x10000, 10, 123)]:
            guard = fake_guard()
            with patch.object(reader.os, 'read', return_value=data), patch.object(reader.os, 'write', return_value=8), patch.object(reader.os, 'close') as close:
                with self.assertRaises(RuntimeError): guard.service(123)
                self.assertTrue(guard.failed)
                if data == event(10, version=2) + event(11):
                    self.assertEqual([c.args[0] for c in close.call_args_list], [10, 11])
                with self.assertRaises(RuntimeError): guard.service(123)
        for failure in [3, OSError('response failed')]:
            guard = fake_guard()
            kwargs = {'side_effect': failure} if isinstance(failure, Exception) else {'return_value': failure}
            with patch.object(reader.os, 'read', return_value=event(10)), patch.object(reader.os, 'write', **kwargs), patch.object(reader.os, 'close') as close:
                with self.assertRaises((RuntimeError, OSError)): guard.service(123)
                close.assert_called_once_with(10)
                self.assertTrue(guard.failed)
        guard = fake_guard()
        with patch.object(reader.os, 'read', side_effect=OSError('monitor failed')):
            with self.assertRaises(OSError): guard.service(123)
        with patch.object(reader.os, 'close') as close:
            guard.close(); guard.close()
            close.assert_called_once_with(99)
        with self.assertRaises(RuntimeError): guard.service(123)

    def test_idle_and_event_storm(self):
        guard = fake_guard()
        with patch.object(reader.os, 'read', side_effect=BlockingIOError()):
            guard.service(None)
        self.assertFalse(guard.failed)
        with patch.object(reader.os, 'read', return_value=event(10)), patch.object(reader.os, 'write', return_value=8), patch.object(reader.os, 'close'):
            with self.assertRaisesRegex(RuntimeError, 'did not drain'): guard.service(123)

    def test_procfs_preconditions(self):
        with tempfile.TemporaryDirectory() as temporary:
            proc = Path(temporary)
            (proc/'mounts').write_text('proc /proc proc rw 0 0\n')
            process = proc/'123'; (process/'fd').mkdir(parents=True)
            file = proc/'fixture'; file.write_bytes(b'hello')
            info = file.stat(); identity = (info.st_dev, info.st_ino)
            (process/'maps').write_text('')
            with patch.object(reader.os, 'geteuid', return_value=0):
                reader.assert_no_existing_readers({identity}, proc)
                (process/'fd/5').symlink_to(file)
                with self.assertRaisesRegex(RuntimeError, 'descriptor'):
                    reader.assert_no_existing_readers({identity}, proc)
                (process/'fd/5').unlink()
                (process/'maps').write_text(f'0-1 r--p 0 {os.major(info.st_dev):x}:{os.minor(info.st_dev):x} {info.st_ino} /fixture\n')
                with self.assertRaisesRegex(RuntimeError, 'mapping'):
                    reader.assert_no_existing_readers({identity}, proc)
                (process/'maps').unlink()
                with self.assertRaises(RuntimeError): reader.assert_no_existing_readers({identity}, proc)
                (proc/'mounts').write_text('proc /proc proc rw,hidepid=2 0 0\n')
                with self.assertRaises(RuntimeError): reader.assert_no_existing_readers(set(), proc)
            with patch.object(reader.os, 'geteuid', return_value=1000):
                with self.assertRaises(RuntimeError): reader.assert_no_existing_readers(set(), proc)

    def test_setup_failures_close_group(self):
        prepared = Mock(directories={'': (7, None, [])}, files={}, binary_identity=(1, 2), binary_parent=8, binary=Path('/target/naru'))
        libc = Mock()
        libc.fanotify_init.return_value = 99
        libc.fanotify_mark.return_value = -1
        with patch.object(reader.platform, 'system', return_value='Linux'), patch.object(reader.ctypes, 'CDLL', return_value=libc), patch.object(reader.os, 'close') as close:
            with self.assertRaises(OSError): reader.ReaderGuard(prepared)
            close.assert_called_once_with(99)
            libc.fanotify_mark.return_value = 0
            with patch.object(reader, 'assert_no_existing_readers', side_effect=PermissionError('hidden')):
                with self.assertRaises(PermissionError): reader.ReaderGuard(prepared)
            self.assertEqual(close.call_count, 2)
            libc.fanotify_init.return_value = -1
            with self.assertRaises(OSError): reader.ReaderGuard(prepared)
            self.assertEqual(close.call_count, 2)

    @unittest.skipUnless(platform.system() == 'Linux', 'requires real wait4')
    def test_collector_failure_after_launch_kills_and_reaps(self):
        guard = Mock()
        guard.service.side_effect = [None, RuntimeError('monitor stopped')]
        launched = []
        real_popen = subprocess.Popen
        def launch(*args, **kwargs):
            child = real_popen(*args, **kwargs); launched.append(child); return child
        with tempfile.TemporaryDirectory() as root, patch.object(collector.subprocess, 'Popen', side_effect=launch):
            with self.assertRaisesRegex(RuntimeError, 'monitor stopped'):
                collector.collect([sys.executable, '-c', 'import time; time.sleep(60)'], root, timeout=2, reader_guard=guard)
        self.assertEqual(len(launched), 1)
        self.assertIsNotNone(launched[0].returncode)
        self.assertTrue(launched[0].stdin.closed)
        with self.assertRaises(ChildProcessError): os.waitpid(launched[0].pid, os.WNOHANG)


@unittest.skipUnless(NATIVE, 'explicit --native Linux/root integration only')
class NativeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='naru-reader-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)/'fixture'; self.root.mkdir(mode=0o700)
        (self.root/'src').mkdir()
        (self.root/'src/input').write_text('synthetic\n')
        self.binary = Path(self.temporary.name)/'python'
        shutil.copyfile(sys.executable, self.binary); self.binary.chmod(0o700)
        self.prepared = PreparedWorkload(self.root, self.binary, binary_digest(self.binary))
        self.addCleanup(self.prepared.close)

    def test_real_threaded_child_and_descriptors(self):
        before = len(list(Path('/proc/self/fd').iterdir()))
        with reader.ReaderGuard(self.prepared) as guard:
            self.assertFalse(os.get_inheritable(guard.fd))
            result = collector.collect([str(self.binary), '-c',
                'import threading; t=threading.Thread(target=lambda: print(open("src/input").read(),end="")); t.start(); t.join()'], self.root, reader_guard=guard)
            self.assertEqual(result['stdout'], 'synthetic\n')
            self.assertGreater(guard.allowed_events, 0)
        self.assertEqual(before, len(list(Path('/proc/self/fd').iterdir())))
        self.prepared.verify_contents()

    def test_existing_fd_and_mapping_are_rejected(self):
        with open(self.root/'src/input', 'rb') as existing:
            with self.assertRaisesRegex(RuntimeError, 'descriptor'):
                reader.ReaderGuard(self.prepared)
            # Python mmap may retain a duplicate fd. Use libc to exercise a
            # mapping with *no* corresponding open descriptor in this process.
            libc = ctypes.CDLL(None, use_errno=True)
            libc.mmap.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_int,
                                  ctypes.c_int, ctypes.c_int, ctypes.c_long]
            libc.mmap.restype = ctypes.c_void_p
            libc.munmap.argtypes = [ctypes.c_void_p, ctypes.c_size_t]
            libc.munmap.restype = ctypes.c_int
            mapping = libc.mmap(None, 10, 1, 2, existing.fileno(), 0)
            self.assertNotEqual(mapping, ctypes.c_void_p(-1).value)
        try:
            with self.assertRaisesRegex(RuntimeError, 'mapping'):
                reader.ReaderGuard(self.prepared)
        finally:
            self.assertEqual(libc.munmap(mapping, 10), 0)

    def test_unexpected_reader_denied_and_selected_child_reaped(self):
        outsider = subprocess.Popen([sys.executable, '-c',
            'import sys; sys.stdin.readline(); print(open(sys.argv[1]).read())', str(self.root/'src/input')],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            with reader.ReaderGuard(self.prepared) as guard:
                service = guard.service
                released = False
                def poll(pid):
                    nonlocal released
                    if pid is not None and not released:
                        outsider.stdin.write(b'go\n'); outsider.stdin.flush(); released = True
                    service(pid)
                with patch.object(guard, 'service', side_effect=poll):
                    with self.assertRaisesRegex(RuntimeError, 'unexpected reader'):
                        collector.collect([str(self.binary), '-c', 'import time; time.sleep(60)'], self.root, timeout=5, reader_guard=guard)
            stdout, _ = outsider.communicate(timeout=5)
            self.assertNotEqual(outsider.returncode, 0)
            self.assertEqual(stdout, b'')
        finally:
            if outsider.poll() is None:
                outsider.kill(); outsider.communicate()

    def test_timeout_exit_and_recovery(self):
        with reader.ReaderGuard(self.prepared) as guard:
            with self.assertRaises(subprocess.TimeoutExpired):
                collector.collect([str(self.binary), '-c', 'import time; time.sleep(60)'], self.root, timeout=.2, reader_guard=guard)
        with reader.ReaderGuard(self.prepared) as guard:
            with self.assertRaises(subprocess.CalledProcessError):
                collector.collect([str(self.binary), '-c', 'raise SystemExit(7)'], self.root, reader_guard=guard)
        with reader.ReaderGuard(self.prepared) as guard:
            self.assertEqual(collector.collect([str(self.binary), '-c', 'print("recovered")'], self.root, reader_guard=guard)['stdout'], 'recovered\n')

    def test_actual_status_measurement(self):
        binary = os.environ.get('NARU_READER_TEST_BINARY')
        self.assertTrue(binary, 'native acceptance requires a test-built naru binary')
        for mode in ['warm-cache', 'first-run']:
            result = subprocess.run([sys.executable, '-B', str(Path(__file__).with_name('measure-status.py')),
                binary, '--reader-guard', '--cache-mode', mode, '--samples', '1', '--files', '4', '--bytes', '4096', '--checks', '2'],
                capture_output=True, text=True, timeout=60, check=True)
            report = json.loads(result.stdout)
            self.assertEqual(report['reader_guard'], 'fanotify-tgid-v1')
            self.assertEqual(report['samples_data'][0]['reader_guard'], 'fanotify-tgid-v1')
            self.assertEqual(report['samples_data'][0]['manifest_sha256'], report['provenance']['manifest_sha256'])


if __name__ == '__main__':
    unittest.main()
