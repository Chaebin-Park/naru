#!/usr/bin/env python3
"""Offline bounded fixtures; fake executables cannot contact a registry."""
import copy
import fcntl
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.dont_write_bytecode = True
import extract

NONCE = "0123456789abcdef0123456789abcdef"
SECRET = "NARU_TEST_SECRET_MUST_NOT_APPEAR"
PACKAGE = {"name":"naru-web-example", "version":"1.0.0", "private":True, "type":"module", "workspaces":["packages/message"], "dependencies":{"naru-web-message":"*"}, "engines":{"node":"22.22.2","npm":"10.9.7"}}
WORKSPACE = {"name":"naru-web-message","version":"1.0.0","private":True,"type":"module","exports":"./index.mjs"}
LOCK = {"name":"naru-web-example","version":"1.0.0","lockfileVersion":3,"requires":True,"packages":{"":{key:PACKAGE[key] for key in ("name","version","workspaces","dependencies","engines")},"packages/message":{"name":"naru-web-message","version":"1.0.0"},"node_modules/naru-web-message":{"link":True,"resolved":"packages/message"}}}
ENVELOPE = {"schema":1,"sample":"naru-web","request_id":NONCE,"tools":{"node":"22.22.2"},"routes":[{"method":"GET","path":"/health"},{"method":"GET","path":"/message"}]}


class Fixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.sample = self.root / "examples/web"
        self.sample.mkdir(parents=True)
        for name in extract.REQUIRED:
            path = self.sample / name
            path.parent.mkdir(parents=True,exist_ok=True)
            path.write_text("export {};\n")
        self.write("package.json", PACKAGE)
        self.write("packages/message/package.json", WORKSPACE)
        self.write("package-lock.json", LOCK)
        (self.sample / ".env.example").write_text("APP_LABEL=public\nPORT=3000\n")
        self.exports = self.root / "exports"
        self.exports.mkdir()

    def write(self, name, value):
        (self.sample / name).write_text(json.dumps(value))

    def routes(self, value=None):
        (self.exports / "web-export.json").write_text(json.dumps(ENVELOPE if value is None else value))
        return extract.validate_routes(self.exports, NONCE)


class SurfaceTests(Fixture):
    def test_routes_mutations_and_reorder(self):
        first = self.routes()
        changed = copy.deepcopy(ENVELOPE)
        changed["routes"].reverse()
        self.assertEqual(first,self.routes(changed))
        changed["routes"].pop()
        self.assertNotEqual(first,self.routes(changed))
        changed["routes"] = []
        self.assertEqual(self.routes(changed)["items"],[])

    def test_workspace_version_requires_lock_update(self):
        first = extract.dependencies(self.sample)
        workspace, lock = copy.deepcopy(WORKSPACE), copy.deepcopy(LOCK)
        workspace["version"] = "2.0.0"
        self.write("packages/message/package.json",workspace)
        with self.assertRaises(ValueError): extract.dependencies(self.sample)
        lock["packages"]["packages/message"]["version"] = "2.0.0"
        self.write("package-lock.json",lock)
        self.assertNotEqual(first,extract.dependencies(self.sample))
        lock["packages"]["packages/message"].pop("name")
        self.write("package-lock.json",lock)
        self.assertEqual(len(extract.dependencies(self.sample)["items"]),2)

    def test_environment_values_keys_and_valid_underscore(self):
        path = self.sample / ".env.example"
        first = extract.environment(path)
        path.write_text("APP_LABEL="+SECRET+"\nPORT='a=b'\n")
        self.assertEqual(first,extract.environment(path))
        path.write_text("_TOKEN="+SECRET+"\r\nPORT=x\r\n")
        result = extract.environment(path)
        self.assertIn("env:_TOKEN",[item["id"] for item in result["items"]])
        self.assertNotEqual(first,result)
        self.assertNotIn(SECRET,json.dumps(result))


class ValidationTests(Fixture):
    def test_bad_envelopes(self):
        edits = [lambda d:d.update(schema=True),lambda d:d.update(sample="other"),lambda d:d.update(request_id="0"*32),lambda d:d.update(extra=1),lambda d:d.pop("tools"),lambda d:d["tools"].update(node="23.0.0"),lambda d:d["routes"].append(d["routes"][0]),lambda d:d["routes"][0].update(path="/../private"),lambda d:d["routes"][0].update(path="/a%20b"),lambda d:d["routes"][0].update(method=[]),lambda d:d.update(routes=[{"method":"GET","path":"/"}]*1025)]
        for index,edit in enumerate(edits):
            with self.subTest(index=index):
                doc=copy.deepcopy(ENVELOPE)
                edit(doc)
                with self.assertRaises(ValueError): self.routes(doc)
        path=self.exports/"web-export.json"
        for data in ('{"schema":1,"schema":1}', '{"schema":NaN}', 'x'*(extract.LIMIT+1)):
            path.write_text(data)
            with self.assertRaises(ValueError): extract.validate_routes(self.exports,NONCE)
        path.unlink()
        with self.assertRaises(ValueError): extract.validate_routes(self.exports,NONCE)

    def test_lock_remote_escape_and_declarations(self):
        edits=[lambda d:d.update(lockfileVersion=True),lambda d:d["packages"].update({"node_modules/remote":{"resolved":"https://"+SECRET}}),lambda d:d["packages"]["node_modules/naru-web-message"].update(resolved="../"+SECRET),lambda d:d["packages"]["node_modules/naru-web-message"].update(integrity=SECRET),lambda d:d["packages"]["packages/message"].update(version="2.0.0"),lambda d:d["packages"][""].update(devDependencies={"remote":SECRET})]
        for index,edit in enumerate(edits):
            with self.subTest(index=index):
                lock=copy.deepcopy(LOCK)
                edit(lock)
                self.write("package-lock.json",lock)
                with self.assertRaises(ValueError) as caught: extract.dependencies(self.sample)
                self.assertNotIn(SECRET,str(caught.exception))

    def test_environment_errors_never_quote_values(self):
        path=self.sample/".env.example"
        cases=["A="+SECRET+"\nA=x", "export A="+SECRET, "A='"+SECRET, "A="+SECRET+"\\", "A="+SECRET+"\x00", SECRET, "A="+SECRET+"\u2028B=x", "A="+SECRET+"\rB=x", "A"*125+"="+SECRET, "A="+SECRET*65536]
        for value in cases:
            path.write_text(value)
            with self.assertRaises(ValueError) as caught: extract.environment(path)
            self.assertNotIn(SECRET,str(caught.exception))
        path.write_text("\n".join("KEY"+str(i)+"=x" for i in range(1025)))
        with self.assertRaises(ValueError): extract.environment(path)


class BuildTests(Fixture):
    def tools(self, failure=False):
        tools=self.root/"tools"
        tools.mkdir(exist_ok=True)
        calls=self.root/"calls"
        body='''import json,os,pathlib,sys
for key in os.environ:
 assert key not in ('NODE_OPTIONS','NPM_TOKEN','HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','SECRET_VALUE') and not key.lower().startswith('npm_config_')
with pathlib.Path(CALLS).open('a') as out: out.write(pathlib.Path(sys.argv[0]).name+' '+json.dumps(sys.argv[1:])+'\\n')
if '--version' in sys.argv: print('22.22.2' if pathlib.Path(sys.argv[0]).name=='bad-node' else 'v22.22.2' if pathlib.Path(sys.argv[0]).name=='node' else '10.9.7')
elif pathlib.Path(sys.argv[0]).name=='npm':
 assert '--offline' in sys.argv and '--ignore-scripts' in sys.argv
 paths=[arg.split('=',1)[1] for arg in sys.argv if arg.startswith(('--userconfig=','--globalconfig='))]
 assert len(set(paths))==2 and all(pathlib.Path(p).read_text()=='' for p in paths)
elif '--test' in sys.argv: pass
else:
 if FAIL: print(SECRET_LITERAL,file=sys.stderr); sys.exit(1)
 out=pathlib.Path(sys.argv[-2]); value=ENVELOPE_LITERAL; value['request_id']=sys.argv[-1]
 (out/'web-export.json').write_text(json.dumps(value))
'''.replace("CALLS",repr(str(calls))).replace("ENVELOPE_LITERAL",repr(ENVELOPE)).replace("FAIL",repr(failure)).replace("SECRET_LITERAL",repr(SECRET))
        for name in ("node","npm"):
            path=tools/name
            path.write_text("#!"+sys.executable+"\n"+body)
            path.chmod(0o700)
        return mock.patch.dict(os.environ,{"PATH":str(tools)+os.pathsep+os.environ.get("PATH", ""),"NODE_OPTIONS":SECRET,"npm_config_userconfig":SECRET,"NPM_TOKEN":SECRET,"HTTP_PROXY":SECRET,"SECRET_VALUE":SECRET})

    def test_all_children_sanitized_and_repeatable(self):
        with self.tools():
            first=extract.build(self.root,timeout=5)
            self.assertEqual(first,extract.build(self.root,timeout=5))
        self.assertEqual(len((self.root/"calls").read_text().splitlines()),10)
        self.assertNotIn(SECRET,json.dumps(first))

    def test_prevalidation_before_any_child(self):
        lock=copy.deepcopy(LOCK)
        lock["packages"]["node_modules/naru-web-message"]["resolved"]="https://"+SECRET
        self.write("package-lock.json",lock)
        with mock.patch.object(extract.subprocess,"Popen") as child:
            with self.assertRaises(ValueError): extract.build(self.root)
            child.assert_not_called()

    def test_failed_build_does_not_reuse_old_export_or_leak(self):
        stale=self.sample/"dist"
        stale.mkdir()
        (stale/"web-export.json").write_text(json.dumps(ENVELOPE))
        with self.tools(failure=True):
            with self.assertRaises(RuntimeError) as caught: extract.build(self.root,timeout=5)
            self.assertNotIn(SECRET,str(caught.exception))
            process=subprocess.run([sys.executable,"-B",str(Path(extract.__file__).resolve()),"routes"],cwd=self.root,env={**os.environ,"NARU_ROOT":str(self.root)},capture_output=True,timeout=8,check=False)
            self.assertNotEqual(process.returncode,0)
            self.assertEqual(process.stdout,b"")
            self.assertNotIn(SECRET.encode(),process.stderr)

    def test_lock_and_source_symlink(self):
        state=self.root/".naru/state"
        state.mkdir(parents=True)
        with (state/"web-build.lock").open("w") as lock:
            fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
            with self.assertRaises(BlockingIOError): extract.build(self.root)
        path=self.sample/"src/routes.mjs"
        path.unlink()
        path.symlink_to(self.sample/"src/server.mjs")
        with self.assertRaises(ValueError): extract.build(self.root)

    def test_sample_ancestor_symlink_rejected(self):
        other=self.root/"outside"
        (self.root/"examples").rename(other)
        (self.root/"examples").symlink_to(other,target_is_directory=True)
        with mock.patch.object(extract.subprocess,"Popen") as child:
            with self.assertRaises(ValueError): extract.build(self.root)
            child.assert_not_called()

    def test_timeout_reaps_owned_child(self):
        marker=self.root/"pid"
        script="import os,time,pathlib; pathlib.Path("+repr(str(marker))+").write_text(str(os.getpid())); time.sleep(30)"
        launch=subprocess.Popen
        def barrier(*args,**kwargs):
            process=launch(*args,**kwargs)
            deadline=time.monotonic()+5
            while not marker.exists() and process.poll() is None and time.monotonic()<deadline: time.sleep(0.02)
            if not marker.exists():
                extract.stop_group(process)
                self.fail("fixture did not start")
            return process
        with mock.patch.object(extract.subprocess,"Popen",side_effect=barrier):
            with self.assertRaises(subprocess.TimeoutExpired):
                extract.run_step([sys.executable,"-c",script],self.root,{"PATH":os.defpath},time.monotonic()+0.1,"fixture")
        with self.assertRaises(ProcessLookupError): os.kill(int(marker.read_text()),0)

    def test_cancel_reaps_owned_process_group(self):
        marker=self.root/"cancel-pids"
        body="import os,time,pathlib,subprocess,signal,sys\nchild=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])\ndef stop(sig,frame):\n child.wait(timeout=1)\n sys.exit(0)\nsignal.signal(signal.SIGTERM,stop)\npathlib.Path("+repr(str(marker))+").write_text(str(os.getpid())+' '+str(child.pid))\ntime.sleep(30)\n"
        with self.tools():
            (self.root/"tools/node").write_text("#!"+sys.executable+"\n"+body)
            process=subprocess.Popen([sys.executable,"-B",str(Path(extract.__file__).resolve()),"routes"],cwd=self.root,env={**os.environ,"NARU_ROOT":str(self.root)},stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            try:
                deadline=time.monotonic()+5
                while not marker.exists() and process.poll() is None and time.monotonic()<deadline: time.sleep(0.02)
                self.assertTrue(marker.exists(),"fixture did not start")
                pids=[int(value) for value in marker.read_text().split()]
                process.terminate()
                out,err=process.communicate(timeout=6)
                self.assertNotEqual(process.returncode,0)
                self.assertEqual(out,b"")
                self.assertNotIn(SECRET.encode(),err)
                for pid in pids:
                    with self.assertRaises(ProcessLookupError): os.kill(pid,0)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate(timeout=2)
                if marker.exists():
                    try: os.killpg(int(marker.read_text().split()[0]),signal.SIGKILL)
                    except ProcessLookupError: pass


if __name__=="__main__":
    unittest.main()
