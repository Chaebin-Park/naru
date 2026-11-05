#!/usr/bin/env python3
"""Bounded standard-library fixtures; no Android toolchain, downloads or devices."""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import signal
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.dont_write_bytecode = True
import extract

NONCE = "0123456789abcdef0123456789abcdef"
XML = b'''<manifest xmlns:a="http://schemas.android.com/apk/res/android" package="example.naru">
<uses-permission a:name="android.permission.INTERNET"/>
<application><activity a:name=".MainActivity" a:exported="true"/></application></manifest>'''


def node(path, version="1.0.0"):
    return {"id": "project:" + path, "kind": "project", "project_path": path, "module": {"group": "example.naru", "name": path[1:], "version": version}, "module_origin": "resolution"}


def envelope(xml=XML):
    return {"schema": 1, "sample": "naru-android", "variant": "debug", "request_id": NONCE, "tools": dict(extract.TOOLS), "manifest": {"file": "debug-merged-manifest.xml", "sha256": "sha256:" + hashlib.sha256(xml).hexdigest()}, "dependencies": {"configuration": "debugRuntimeClasspath", "root": node(":app"), "components": [node(":library")], "edges": [{"from": "project::app", "to": "project::library"}]}}


class Fixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def validate(self, doc=None, xml=XML):
        (self.root / "debug-export.json").write_text(json.dumps(doc if doc is not None else envelope(xml)), encoding="utf-8")
        (self.root / "debug-merged-manifest.xml").write_bytes(xml)
        return extract.validate_exports(self.root, NONCE)


class SurfaceTests(Fixture):
    def test_valid_and_mutations(self):
        base = self.validate()
        self.assertIn("debug:permission:android.permission.INTERNET", [x["id"] for x in base["manifest"]["items"]])
        self.assertNotEqual(base["manifest"], self.validate(xml=XML.replace(b"INTERNET", b"CAMERA"))["manifest"])
        self.assertNotEqual(base["manifest"], self.validate(xml=XML.replace(b'exported="true"', b'exported="false"'))["manifest"])
        doc = envelope()
        doc["dependencies"]["components"][0]["module"]["version"] = "2.0.0"
        self.assertNotEqual(base["dependencies"], self.validate(doc)["dependencies"])
        doc["dependencies"]["components"] = []
        doc["dependencies"]["edges"] = []
        result = self.validate(doc)["dependencies"]
        self.assertNotEqual(base["dependencies"], result)
        self.assertEqual(len(result["items"]), 1)

    def test_reorder(self):
        doc = envelope()
        doc["dependencies"]["components"].append(node(":other"))
        doc["dependencies"]["edges"].append({"from":"project::app", "to":"project::other"})
        first = self.validate(doc)
        doc["dependencies"]["components"].reverse()
        doc["dependencies"]["edges"].reverse()
        self.assertEqual(first, self.validate(doc))

    def test_manifest_alias_provider_projection(self):
        xml = XML.replace(b"<application>", b'<application a:permission="example.READ">').replace(b"</application>", b'<activity-alias a:name="Alias" a:targetActivity=".MainActivity" a:exported="false"/><provider a:name="Provider" a:exported="false" a:authorities="example.b;example.a" a:readPermission="example.READ_PROVIDER"/></application>')
        values = [item["value"] for item in self.validate(xml=xml)["manifest"]["items"]]
        provider = next(value for value in values if value.get("kind") == "provider")
        self.assertEqual(provider["permission"], "example.READ")
        self.assertEqual(provider["authorities"], ["example.a", "example.b"])
        self.assertEqual(provider["read_permission"], "example.READ_PROVIDER")
        self.assertIsNone(provider["write_permission"])


class ValidationTests(Fixture):
    def test_envelope_and_graph_rejections(self):
        changes = [
            lambda d: d.update(variant="release"), lambda d: d.update(request_id="f"*32),
            lambda d: d.update(schema=True), lambda d: d.update(extra=1), lambda d: d.pop("tools"),
            lambda d: d["tools"].update(java_major=True),
            lambda d: d["manifest"].update(file="../manifest.xml"),
            lambda d: d["manifest"].update(sha256="sha256:"+"0"*64),
            lambda d: d["dependencies"]["components"].append(copy.deepcopy(d["dependencies"]["root"])),
            lambda d: d["dependencies"]["edges"].append(copy.deepcopy(d["dependencies"]["edges"][0])),
            lambda d: d["dependencies"]["edges"][0].update(to="project::missing"),
            lambda d: d["dependencies"].update(edges=[]),
            lambda d: d["dependencies"]["components"][0].update(kind="unresolved"),
            lambda d: d["dependencies"]["components"][0].update(module_origin="invented"),
            lambda d: d["dependencies"]["components"][0]["module"].update(version="unspecified"),
        ]
        for change in changes:
            with self.subTest(change=changes.index(change)):
                doc = envelope()
                change(doc)
                with self.assertRaises(ValueError): self.validate(doc)

    def test_files_and_json(self):
        self.validate()
        path = self.root / "debug-export.json"
        for data in ('{"schema":1,"schema":1}', '{"schema":NaN}', " "*(extract.LIMIT+1)):
            path.write_text(data)
            with self.assertRaises(ValueError): extract.validate_exports(self.root, NONCE)
        self.validate()
        path.unlink()
        with self.assertRaises(ValueError): extract.validate_exports(self.root, NONCE)
        self.validate()
        path.unlink()
        path.symlink_to(self.root / "debug-merged-manifest.xml")
        with self.assertRaises(ValueError): extract.validate_exports(self.root, NONCE)

    def test_xml_rejections(self):
        invalid = [
            b'<!DOCTYPE manifest [<!ENTITY x "bad">]>'+XML,
            XML.replace(b'exported="true"', b'exported="yes"'),
            XML.replace(b' a:exported="true"', b''),
            XML.replace(b'.MainActivity', b'${unresolved}'),
            XML.replace(b'<uses-permission ', b'<uses-permission-sdk-23 '),
            XML.replace(b'<application>', b'<uses-permission a:name="android.permission.INTERNET"/><application>'),
            XML.replace(b'<application>', b'<uses-permission a:name="example.TEST" a:maxSdkVersion="0"/><application>'),
            b'\xff'+XML,
        ]
        for xml in invalid:
            with self.subTest(xml=xml[:50]):
                with self.assertRaises((ValueError, extract.ET.ParseError)): self.validate(xml=xml)


class BuildTests(Fixture):
    def sample(self):
        sdk = self.root / "sdk"
        for relative in ("platforms/android-35/android.jar", "build-tools/35.0.0/aapt2", "build-tools/35.0.0/source.properties"):
            path = sdk / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("Pkg.Revision=35.0.0\n")
        environment = mock.patch.dict(os.environ, {"ANDROID_HOME": str(sdk)})
        environment.start()
        self.addCleanup(environment.stop)
        for name in extract.ROOT_FILES:
            path = self.root / "examples/android" / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("// synthetic fixture\n")
        for module in ("app", "library"):
            path = self.root / "examples/android" / module / "src/main/AndroidManifest.xml"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(XML)

    def fake_gradle(self, body):
        tools = self.root / "tools"
        tools.mkdir(exist_ok=True)
        program = tools / "gradle"
        program.write_text("#!"+sys.executable+"\n"+body)
        program.chmod(0o700)
        return mock.patch.dict(os.environ, {"PATH":str(tools)+os.pathsep+os.environ.get("PATH", "")})

    def test_build_failure_cannot_reuse_stale_export(self):
        self.sample()
        stale = self.root / "examples/android/build"
        stale.mkdir()
        (stale / "debug-export.json").write_text(json.dumps(envelope()))
        with self.fake_gradle("import sys\nsys.exit(1)\n"):
            with self.assertRaisesRegex(RuntimeError,"Gradle build failed"): extract.build(self.root, timeout=2)
        with self.fake_gradle("pass\n"):
            with self.assertRaisesRegex(ValueError,"export directory"): extract.build(self.root, timeout=2)

    def test_fresh_request_and_source_copy(self):
        self.sample()
        body = '''import sys,json,hashlib,pathlib
args=sys.argv[1:]
root=pathlib.Path(args[args.index('-p')+1])
assert not (root/'build').exists()
assert not (root/'local.properties').exists()
out=pathlib.Path(next(a.split('=',1)[1] for a in args if a.startswith('-PnaruOutput=')))
nonce=next(a.split('=',1)[1] for a in args if a.startswith('-PnaruRequestId='))
xml=XML_LITERAL
doc=DOC_LITERAL
doc['request_id']=nonce
(out/'debug-merged-manifest.xml').write_bytes(xml)
(out/'debug-export.json').write_text(json.dumps(doc))
'''.replace("XML_LITERAL",repr(XML)).replace("DOC_LITERAL",repr(envelope()))
        with self.fake_gradle(body):
            first = extract.build(self.root, timeout=2)
            self.assertEqual(first, extract.build(self.root, timeout=2))

    def test_missing_sdk_does_not_launch_gradle(self):
        self.sample()
        with mock.patch.dict(os.environ, {"ANDROID_HOME": str(self.root / "absent-sdk")}), mock.patch.object(extract.subprocess, "Popen") as launch:
            with self.assertRaisesRegex(ValueError, "already be installed"):
                extract.build(self.root, timeout=1)
            launch.assert_not_called()

    def test_lock_and_symlink(self):
        self.sample()
        state = self.root / ".naru/state"
        state.mkdir(parents=True)
        with (state / "android-build.lock").open("w") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaises(BlockingIOError): extract.build(self.root, timeout=1)
        path = self.root / "examples/android/app/src/main/AndroidManifest.xml"
        path.unlink()
        path.symlink_to(self.root / "examples/android/library/src/main/AndroidManifest.xml")
        with self.assertRaises(ValueError): extract.build(self.root, timeout=1)

    def test_sample_ancestor_symlink_rejected_before_copy(self):
        self.sample()
        examples = self.root / "examples"
        external = self.root / "external-examples"
        examples.rename(external)
        examples.symlink_to(external, target_is_directory=True)
        destination = self.root / "copy"
        destination.mkdir()
        with self.assertRaisesRegex(ValueError, "source directory"):
            extract.copy_sample(self.root, destination)
        self.assertEqual(list(destination.iterdir()), [])

    def test_timeout_reaps_owned_process(self):
        self.sample()
        pidfile = self.root / "pid"
        childfile = self.root / "child"
        body = "import os,time,pathlib,subprocess,signal,sys\nchild=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])\npathlib.Path("+repr(str(childfile))+").write_text(str(child.pid))\ndef stop(sig,frame):\n child.wait(timeout=1)\n sys.exit(0)\nsignal.signal(signal.SIGTERM,stop)\npathlib.Path("+repr(str(pidfile))+").write_text(str(os.getpid()))\ntime.sleep(30)\n"
        started = time.monotonic()
        launch = subprocess.Popen
        def started_process(*args, **kwargs):
            process = launch(*args, **kwargs)
            deadline = time.monotonic()+5
            while not pidfile.exists() and process.poll() is None and time.monotonic()<deadline:
                time.sleep(0.02)
            if not pidfile.exists():
                extract.stop_group(process)
                self.fail("bounded fixture startup failed")
            return process
        with self.fake_gradle(body), mock.patch.object(extract.subprocess, "Popen", side_effect=started_process):
            with self.assertRaises(subprocess.TimeoutExpired): extract.build(self.root, timeout=0.1)
        self.assertLess(time.monotonic()-started, 10)
        pid = int(pidfile.read_text())
        with self.assertRaises(ProcessLookupError): os.kill(pid, 0)
        with self.assertRaises(ProcessLookupError): os.kill(int(childfile.read_text()), 0)

    def test_cancellation_reaps_owned_process(self):
        self.sample()
        pidfile = self.root / "cancel-pid"
        body = "import os,time,pathlib\npathlib.Path("+repr(str(pidfile))+").write_text(str(os.getpid()))\ntime.sleep(30)\n"
        with self.fake_gradle(body):
            process = subprocess.Popen([sys.executable, "-B", str(Path(extract.__file__).resolve()), "manifest"], cwd=self.root, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env={**os.environ, "NARU_ROOT": str(self.root)})
            try:
                deadline = time.monotonic()+5
                while not pidfile.exists() and process.poll() is None and time.monotonic()<deadline:
                    time.sleep(0.02)
                self.assertTrue(pidfile.exists(), "build did not start")
                process.terminate()
                out, err = process.communicate(timeout=6)
                self.assertNotEqual(process.returncode, 0)
                self.assertEqual(out, b"")
                self.assertIn(b"interrupted", err)
                with self.assertRaises(ProcessLookupError): os.kill(int(pidfile.read_text()), 0)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.communicate(timeout=2)
                if pidfile.exists():
                    try: os.killpg(int(pidfile.read_text()), signal.SIGKILL)
                    except ProcessLookupError: pass


if __name__ == "__main__":
    unittest.main()
