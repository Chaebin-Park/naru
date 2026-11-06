"""Regression checks for candidate source isolation; only temporary repositories."""
import os
from pathlib import Path
import platform
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('prepare-candidate.sh').resolve()
GO = shutil.which('go')

class CandidateIsolation(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='naru-candidate-test-')
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.repo = self.base / 'repo'
        self.repo.mkdir()
        self.output = self.base / 'output'
        self.env = dict(os.environ, GOWORK='off', GOFLAGS='', GOENV='off', GOTOOLCHAIN='local')
        self.git('init', '-q')
        (self.repo / 'go.mod').write_text('module github.com/Chaebin-Park/naru\n\ngo 1.23\n')
        (self.repo / 'LICENSE').write_text('Synthetic fixture\n')
        (self.repo / 'cmd/naru').mkdir(parents=True)
        self.main = self.repo / 'cmd/naru/main.go'
        self.main.write_text('package main\nimport "fmt"\nfunc main(){fmt.Println("committed-source")}\n')
        self.git('add', '.')
        self.git('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'fixture')

    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.repo, text=True)

    def build(self):
        r = subprocess.run(['sh', str(SCRIPT), str(self.output)], cwd=self.repo,
                           env=self.env, capture_output=True, text=True, timeout=120)
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        osname = {'Darwin':'darwin','Linux':'linux'}[platform.system()]
        arch = {'arm64':'arm64','aarch64':'arm64','x86_64':'amd64'}[platform.machine()]
        actual = subprocess.check_output([str(self.output / f'naru-{osname}-{arch}')], text=True)
        self.assertEqual(actual.strip(), 'committed-source')
        self.assertEqual((self.output/'REVISION').read_text().strip(), self.git('rev-parse','HEAD').strip())

    def test_ignored_go_source_is_excluded(self):
        (self.repo/'.git/info/exclude').write_text('cmd/naru/ignored.go\n')
        (self.repo/'cmd/naru/ignored.go').write_text('package main\nimport "fmt"\nfunc init(){fmt.Println("ignored-source-injected")}\n')
        self.assertEqual(self.git('status','--porcelain'), '')
        self.build()

    def test_ambient_source_overrides_are_disabled(self):
        self.env.update(GOFLAGS='-overlay='+str(self.base/'missing-overlay.json'),
                        GOWORK=str(self.base/'missing-workspace.work'),
                        GOTOOLCHAIN='not-an-installed-toolchain')
        self.build()

    def test_live_edit_after_snapshot_does_not_enter_build(self):
        wrapper = self.base/'bin';wrapper.mkdir()
        # The script records its toolchain after capturing source. Change only the
        # original fixture when that command runs, then invoke the real compiler.
        import shlex
        (wrapper/'go').write_text('#!/bin/sh\nif [ "$1" = version ]; then\n'
            + 'printf %s '+shlex.quote('package main\nimport "fmt"\nfunc main(){fmt.Println("late-source")}\n')
            + ' > '+shlex.quote(str(self.main))+'\nfi\nexec '+shlex.quote(GO)+' "$@"\n')
        (wrapper/'go').chmod(0o700)
        self.env['PATH']=str(wrapper)+os.pathsep+self.env['PATH']
        self.build()

if __name__ == '__main__':
    unittest.main()
