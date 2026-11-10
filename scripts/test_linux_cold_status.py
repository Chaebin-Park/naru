"""Protocol fault tests; native mode checks probing/guarding without OS resets."""
import contextlib
import os
from pathlib import Path
import platform
import shutil
import sys
import tempfile
import unittest
from unittest.mock import MagicMock, Mock, patch

import linux_cold_status as cold
import linux_reader_guard as reader
import linux_status_measure as collector
from linux_cache_probe import Fixture
from status_provenance import PreparedWorkload, binary_digest

NATIVE = '--native' in sys.argv
if NATIVE:
    sys.argv.remove('--native')
    if platform.system() != 'Linux' or os.geteuid() != 0:
        raise SystemExit('--native requires Linux root')


def snapshot(start, resident):
    return {'scope': 'guest-input-file-page-cache-snapshot', 'files': 1, 'pages': 1,
            'resident_pages': resident, 'started_ns': start, 'finished_ns': start + 1}


class ColdTests(unittest.TestCase):
    def setUp(self):
        self.session = cold.GuestSession.__new__(cold.GuestSession)
        self.session.reset = Mock(return_value=None)
        self.session.validate = Mock()
        self.session.boot_id = '00000000-0000-4000-8000-000000000000'
        self.session.environment = {'controller': 'synthetic test only'}
        self.session.attempts = []
        self.prepared = Mock(root=Path('/synthetic'), binary=Path('/trusted/naru'),
                             files={'src/input': ((0, 0, 0, 0, 0, 1), 'sha')}, manifest_sha256='digest')
        self.fixture = MagicMock()
        self.fixture.__enter__.return_value = self.fixture
        self.fixture.__exit__.return_value = False
        self.fixture.files = {'input': (80, (0, 0, 0, 0, 0, 1))}
        self.fixture.snapshot.side_effect = [snapshot(10, 1), snapshot(60, 0), snapshot(80, 0)]
        self.post = MagicMock()
        self.post.__enter__.return_value = self.post
        self.post.__exit__.return_value = False
        self.post.snapshot.return_value = snapshot(120, 1)
        self.guard = MagicMock(allowed_events=3)
        self.guard.__enter__.return_value = self.guard
        self.guard.__exit__.return_value = False
        self.sample = {'stdout': '{}', 'started_ns': 100, 'finished_ns': 110,
                       'seconds': 1e-8, 'max_rss_bytes': 4096}
        self.stack = contextlib.ExitStack(); self.addCleanup(self.stack.close)
        def enter(target, **kwargs): return self.stack.enter_context(patch(target, **kwargs))
        self.factory = enter('linux_cold_status.Fixture', side_effect=[self.fixture, self.post])
        self.guard_factory = enter('linux_cold_status.ReaderGuard', return_value=self.guard)
        enter('linux_cold_status.time.monotonic_ns', side_effect=[20, 30, 40, 50])
        self.fsync = enter('linux_cold_status.os.fsync')
        self.sync = enter('linux_cold_status.os.sync')
        self.collect = enter('linux_cold_status.collect', return_value=self.sample)

    def test_order_coverage_binding_and_closed_probe(self):
        sequence = []
        self.fsync.side_effect = lambda fd: sequence.append('fsync')
        self.sync.side_effect = lambda: sequence.append('sync')
        self.session.reset.side_effect = lambda: sequence.append('reset')
        def probe():
            values = [snapshot(10, 1), snapshot(60, 0), snapshot(80, 0)]
            i = sum(x == 'probe' for x in sequence)
            sequence.append('probe'); return values[i]
        self.fixture.snapshot.side_effect = probe
        self.fixture.close.side_effect = lambda: sequence.append('close')
        def collect(*args, **kwargs):
            self.assertEqual(sequence[-1], 'close')
            self.assertEqual(kwargs['probe_finished_ns'], 81)
            self.assertIs(kwargs['reader_guard'], self.guard)
            sequence.append('collect'); return self.sample
        self.collect.side_effect = collect
        result = cold.collect_guarded(self.prepared, self.session)
        self.assertEqual(sequence, ['probe', 'fsync', 'sync', 'reset', 'probe', 'probe', 'close', 'collect'])
        self.guard_factory.assert_called_once_with(self.prepared, probe_fds=(80,))
        proof = result['cache_transition']
        self.assertEqual(proof['probe_to_collector_start_ns'], 19)
        self.assertEqual(proof['manifest_sha256'], 'digest')
        self.assertEqual(proof['max_gap_ns'], 100_000_000)
        self.assertTrue(proof['collection_complete'])
        self.assertEqual(len(self.session.attempts), 1)

    def test_reset_failure_cannot_collect(self):
        self.session.reset.side_effect = OSError('reset failed')
        with self.assertRaises(OSError): cold.collect_guarded(self.prepared, self.session)
        self.collect.assert_not_called()
        self.guard.__exit__.assert_called_once()
        self.fixture.__exit__.assert_called_once()
        self.assertFalse(self.session.attempts[0]['collection_complete'])
        self.assertIn('reset failed', self.session.attempts[0]['error'])

    def test_reset_claim_is_not_completion(self):
        self.session.reset.return_value = {'success': True}
        with self.assertRaises(ValueError): cold.collect_guarded(self.prepared, self.session)
        self.collect.assert_not_called()

    def test_resident_partial_invalid_and_out_of_order_probe(self):
        for bad in [snapshot(60, 1), {**snapshot(60, 0), 'pages': 2},
                    {**snapshot(60, 0), 'resident_pages': True},
                    {**snapshot(60, 0), 'files': 0},
                    {**snapshot(60, 0), 'scope': 'claimed'},
                    {**snapshot(60, 0), 'finished_ns': 59}]:
            with self.subTest(bad=bad):
                self.fixture.snapshot.side_effect = [snapshot(10, 1), bad]
                self.factory.side_effect = [self.fixture, self.post]
                # Each attempt uses a fresh clock; failures never reach collect.
                with patch.object(cold.time, 'monotonic_ns', side_effect=[20, 30, 40, 50]):
                    with self.assertRaises(ValueError): cold.collect_guarded(self.prepared, self.session)
                self.collect.assert_not_called()

    def test_monitor_provenance_and_close_errors_block_launch(self):
        for target in [self.guard.service, self.prepared.validate_metadata, self.fixture.close]:
            with self.subTest(target=target):
                target.side_effect = OSError('blocked')
                self.factory.side_effect = [self.fixture, self.post]
                self.fixture.snapshot.side_effect = [snapshot(10, 1), snapshot(60, 0), snapshot(80, 0)]
                with patch.object(cold.time, 'monotonic_ns', side_effect=[20, 30, 40, 50]):
                    with self.assertRaises(OSError): cold.collect_guarded(self.prepared, self.session)
                self.collect.assert_not_called()
                target.side_effect = None

    def test_fixed_gap_and_timestamp_bounds(self):
        trace = {'prepared_probe': snapshot(10, 1), 'sync_started_ns': 20, 'sync_finished_ns': 30,
                 'reset_started_ns': 40, 'reset_finished_ns': 50,
                 'first_probe': snapshot(60, 0), 'second_probe': snapshot(80, 0)}
        self.assertEqual(cold._ordered(trace, {**self.sample, 'started_ns': 100_000_081, 'finished_ns': 100_000_082}), 100_000_000)
        for start in [80, 100_000_082, True, -1]:
            with self.subTest(start=start), self.assertRaises(ValueError):
                cold._ordered(trace, {**self.sample, 'started_ns': start})
        with patch.object(collector.platform, 'system', return_value='Linux'), patch.object(collector.os, 'wait4', create=True), patch.object(collector.time, 'monotonic_ns', return_value=100_000_082), patch.object(collector.subprocess, 'Popen') as popen:
            for end in [81, 100_000_083, True]:
                with self.assertRaises(ValueError): collector.collect(['unused'], '.', probe_finished_ns=end)
            popen.assert_not_called()

    def test_requires_live_boot_bound_guest(self):
        with self.assertRaises(ValueError): cold.collect_guarded(self.prepared, {'reset': True})
        with self.assertRaises(ValueError): cold.GuestSession({'reset': True}, self.session.boot_id, {'vm': 'claimed'})
        with patch.object(cold.platform, 'system', return_value='Linux'), patch.object(cold.os, 'geteuid', return_value=0), patch.object(cold.Path, 'read_text', return_value=self.session.boot_id):
            session = cold.GuestSession(lambda: None, self.session.boot_id, {'controller': 'test'})
            with patch.object(cold.Path, 'read_text', return_value='changed'):
                with self.assertRaises(RuntimeError): session.validate()
        with patch.object(cold.platform, 'system', return_value='Darwin'):
            with self.assertRaises(RuntimeError): cold.GuestSession(lambda: None, self.session.boot_id, {'controller': 'test'})


@unittest.skipUnless(NATIVE, 'explicit native Linux/root test; never resets caches')
class NativeTests(unittest.TestCase):
    def test_real_probe_descriptors_and_external_descriptor_rejection(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)/'project'; root.mkdir(mode=0o700)
            (root/'src').mkdir(mode=0o700)
            (root/'src/input').write_bytes(b'a' * 4096)
            binary = Path(temporary)/'python'
            shutil.copyfile(sys.executable, binary); binary.chmod(0o700)
            with PreparedWorkload(root, binary, binary_digest(binary)) as prepared:
                with Fixture(root/'src') as fixture:
                    probe_fds = tuple(r[0] for r in fixture.files.values())
                    with reader.ReaderGuard(prepared, probe_fds=probe_fds) as guard:
                        probe = fixture.snapshot()
                        self.assertEqual(probe['resident_pages'], probe['pages'])
                        fixture.close()
                        result = collector.collect([str(binary), '-c', 'print(open("src/input").read())'], root, reader_guard=guard)
                        self.assertIn('aaaa', result['stdout'])
                    with open(root/'src/input') as extra:
                        with self.assertRaisesRegex(RuntimeError, 'descriptor'):
                            reader.ReaderGuard(prepared)
                with open(root/'src/input', 'r+b') as writable:
                    with self.assertRaises(ValueError): reader.ReaderGuard(prepared, probe_fds=(writable.fileno(),))
                prepared.verify_contents()


if __name__ == '__main__':
    unittest.main()
