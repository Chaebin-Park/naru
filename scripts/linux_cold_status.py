"""Controller-only isolated-guest input-page-cold protocol; no reset command/CLI.

The external trusted controller owns isolation and supplies a live reset callback.
A dictionary, guest root or claimed UUID alone cannot authorize that operation.
"""
import os
from pathlib import Path
import platform
import time
import uuid

from linux_cache_probe import Fixture, require_nonresident
from linux_reader_guard import ReaderGuard
from linux_status_measure import collect


class GuestSession:
    """Live capability supplied by an already host-verified disposable VM controller.

    This object does not discover/verify host isolation. The callback is trusted
    code, not serialized evidence. Nothing in this module writes drop_caches.
    """
    def __init__(self, reset, expected_boot_id, environment):
        if not callable(reset) or not isinstance(environment, dict) or not environment:
            raise ValueError('cold session requires a live reset callback and controller context')
        self.reset = reset
        self.boot_id = str(uuid.UUID(expected_boot_id))
        self.environment = dict(environment)
        self.attempts = []
        self.validate()

    def validate(self):
        if platform.system() != 'Linux' or os.geteuid() != 0:
            raise RuntimeError('cold session requires the verified Linux root guest')
        actual = Path('/proc/sys/kernel/random/boot_id').read_text().strip()
        if actual != self.boot_id:
            raise RuntimeError('cold session boot identity changed')


def _require_warm(snapshot):
    if (type(snapshot['pages']) is not int or snapshot['pages'] <= 0
            or type(snapshot['resident_pages']) is not int
            or snapshot['resident_pages'] != snapshot['pages']):
        raise ValueError('prepared/re-read input pages must all be resident')
    try:
        require_nonresident(snapshot)
    except ValueError:
        return
    raise ValueError('resident input was incorrectly accepted as nonresident')


def _snapshot(fixture, files, pages, cold):
    snapshot = fixture.snapshot()
    if (type(snapshot['files']) is not int or snapshot['files'] != files
            or type(snapshot['pages']) is not int or snapshot['pages'] != pages
            or snapshot['scope'] != 'guest-input-file-page-cache-snapshot'
            or any(type(snapshot[key]) is not int or snapshot[key] < 0
                   for key in ('started_ns', 'finished_ns'))
            or snapshot['started_ns'] > snapshot['finished_ns']):
        raise ValueError('probe coverage or timestamps are invalid')
    if cold:
        require_nonresident(snapshot)
    else:
        _require_warm(snapshot)
    return snapshot


def _ordered(trace, sample):
    points = [trace['prepared_probe']['started_ns'], trace['prepared_probe']['finished_ns'],
              trace['sync_started_ns'], trace['sync_finished_ns'],
              trace['reset_started_ns'], trace['reset_finished_ns'],
              trace['first_probe']['started_ns'], trace['first_probe']['finished_ns'],
              trace['second_probe']['started_ns'], trace['second_probe']['finished_ns'],
              sample['started_ns'], sample['finished_ns']]
    if (any(type(value) is not int or value < 0 for value in points)
            or points != sorted(points)):
        raise ValueError('cold transition timestamps are invalid or out of order')
    gap = sample['started_ns'] - trace['second_probe']['finished_ns']
    if gap > 100_000_000:
        raise ValueError('probe-to-collector-start gap exceeds 100ms')
    return gap


def collect_guarded(prepared, session=None):
    """Use one collector for guarded warm and per-reset cold samples.

    Probe FDs open before guard/reset, are never inherited, and close before
    launch. Pre-existing mappings remain forbidden even for the controller.
    """
    command = [str(prepared.binary), 'status', '--json']
    if session is None:
        with ReaderGuard(prepared) as guard:
            sample = collect(command, prepared.root, reader_guard=guard)
            if guard.allowed_events == 0:
                raise RuntimeError('reader guard observed no selected-child permission events')
            sample['reader_guard_events'] = guard.allowed_events
        return sample
    if type(session) is not GuestSession:
        raise ValueError('cold collection requires a live GuestSession')
    session.validate()
    trace = {'scope': 'guest-input-file-page-cache-prelaunch', 'boot_id': session.boot_id,
             'environment': dict(session.environment), 'max_gap_ns': 100_000_000,
             'manifest_sha256': prepared.manifest_sha256, 'collection_complete': False}
    session.attempts.append(trace)
    try:
        with Fixture(prepared.root / 'src') as fixture:
            # The fixture must be exactly the prepared nonempty input inventory.
            expected = {'src/' + name for name in fixture.files}
            if expected != {name for name in prepared.files if name.startswith('src/')}:
                raise ValueError('cold probe does not cover the prepared input inventory')
            files = len(fixture.files)
            page_size = os.sysconf('SC_PAGE_SIZE')
            pages = sum((r[1][5] + page_size - 1) // page_size for r in fixture.files.values())
            with ReaderGuard(prepared, probe_fds=tuple(r[0] for r in fixture.files.values())) as guard:
                trace['prepared_probe'] = _snapshot(fixture, files, pages, False)
                trace['sync_started_ns'] = time.monotonic_ns()
                for fd, _ in fixture.files.values():
                    os.fsync(fd)
                os.sync()
                trace['sync_finished_ns'] = time.monotonic_ns()
                session.validate()
                guard.service(None)
                trace['reset_started_ns'] = time.monotonic_ns()
                if session.reset() is not None:
                    raise ValueError('reset callback must complete, not return claimed evidence')
                trace['reset_finished_ns'] = time.monotonic_ns()
                prepared.validate_metadata()
                trace['first_probe'] = _snapshot(fixture, files, pages, True)
                trace['second_probe'] = _snapshot(fixture, files, pages, True)
                _ordered(trace, {'started_ns': trace['second_probe']['finished_ns'],
                                 'finished_ns': trace['second_probe']['finished_ns']})
                fixture.close()  # Checked cleanup; no probe fd/mapping reaches status.
                guard.service(None)
                sample = collect(command, prepared.root, reader_guard=guard,
                                 probe_finished_ns=trace['second_probe']['finished_ns'])
                trace['probe_to_collector_start_ns'] = _ordered(trace, sample)
                if guard.allowed_events == 0:
                    raise RuntimeError('reader guard observed no selected-child permission events')
                sample['reader_guard_events'] = guard.allowed_events
        session.validate()
        # Outside timing and after removing marks: prove a real re-read is seen.
        with Fixture(prepared.root / 'src') as fixture:
            trace['post_status_probe'] = _snapshot(fixture, files, pages, False)
            if trace['post_status_probe']['started_ns'] < sample['finished_ns']:
                raise ValueError('post-status probe precedes collection finish')
        trace['collector_started_ns'] = sample['started_ns']
        trace['collector_finished_ns'] = sample['finished_ns']
        trace['collection_complete'] = True
        sample['cache_transition'] = dict(trace)
        return sample
    except BaseException as error:
        trace['error'] = type(error).__name__ + ': ' + str(error)
        raise
