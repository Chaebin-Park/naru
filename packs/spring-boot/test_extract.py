"""Public synthetic contract fixtures; no Maven or application service required."""
import contextlib
import io
import json
import os
import subprocess
import sys
from unittest import mock
from pathlib import Path
import tempfile
import unittest

import extract

FIXTURES = Path(__file__).resolve().parents[2] / "internal/cli/testdata/spring-boot"


class ContractFixtures(unittest.TestCase):
    def test_contract_changes(self):
        for kind in ("api", "routes"):
            before = {x["id"]: x["value"] for x in extract.exports(FIXTURES / "normal", kind)}
            after = {x["id"]: x["value"] for x in extract.exports(FIXTURES / "broken", kind)}
            self.assertNotEqual(before, after, kind)
            if kind == "api":
                self.assertEqual(before["contract:GET:/greeting"]["status"], 200)
                self.assertEqual(after["contract:GET:/greeting"]["status"], 404)
            else:
                self.assertEqual(after["contract:Greeting/greeting"]["paths"], ["/renamed"])

    def test_resources(self):
        with tempfile.TemporaryDirectory() as root:
            project = Path(root)
            resources = project / "src/main/resources"
            migration = resources / "db/migration/V1__example.sql"
            migration.parent.mkdir(parents=True)
            migration.write_text("CREATE TABLE example (id INT);", encoding="utf-8")
            config = resources / "application.properties"
            config.write_text("example.token=synthetic-secret-never-output\n", encoding="utf-8")
            before = extract.resource_items(project, "migrations")
            migration.write_text("CREATE TABLE example (id BIGINT);", encoding="utf-8")
            self.assertNotEqual(before, extract.resource_items(project, "migrations"))
            before = extract.resource_items(project, "config")
            self.assertNotIn("synthetic-secret", json.dumps(before))
            config.write_text("example.token=another-secret\n", encoding="utf-8")
            self.assertEqual(before, extract.resource_items(project, "config"))
            config.write_text("example.renamed=another-secret\n", encoding="utf-8")
            self.assertNotEqual(before, extract.resource_items(project, "config"))

    def test_invalid_exports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(FileNotFoundError):
                extract.exports(root, "api")
            for raw in ('{"schema":1,"schema":1,"items":[]}', '{"schema":1,"items":[]}', '{"schema":true,"items":[]}', '{"schema":1,"items":[{"id":"wrong","value":{"profile":"extra"}}]}', '{"schema":1,"items":[{"id":"x","value":{}} , {"id":"x","value":{}}]}', '{"schema":1,"items":[{"id":"contract:x","value":{"n":NaN}}]}'):
                (root / "contract-api.json").write_text(raw, encoding="utf-8")
                with self.assertRaises(ValueError):
                    extract.exports(root, "api")
            (root / "contract-api.json").write_bytes(b"x" * (extract.LIMIT + 1))
            with self.assertRaises(ValueError):
                extract.exports(root, "api")

    def test_no_old_exports_or_parallel_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stale = root / "examples/spring-boot/target"
            stale.mkdir(parents=True)
            (stale / "contract-api.json").write_text('{"schema":1,"items":[]}', encoding="utf-8")
            with mock.patch.dict(os.environ, {"NARU_ROOT": directory}), mock.patch.object(sys, "argv", ["extract.py", "api"]):
                output = io.StringIO()
                with mock.patch.object(subprocess, "run"), contextlib.redirect_stdout(output):
                    with self.assertRaises(FileNotFoundError):
                        extract.main()
                self.assertEqual(output.getvalue(), "")
                for error in (subprocess.CalledProcessError(1, ["mvn"]), subprocess.TimeoutExpired(["mvn"], 240)):
                    with mock.patch.object(subprocess, "run", side_effect=error), contextlib.redirect_stdout(output):
                        with self.assertRaises(subprocess.SubprocessError):
                            extract.main()
                self.assertEqual(output.getvalue(), "")
                state = root / ".naru/state"
                self.assertEqual([p.name for p in state.iterdir()], ["spring-boot-build.lock"])
                with (state / "spring-boot-build.lock").open("a") as lock:
                    extract.fcntl.flock(lock, extract.fcntl.LOCK_EX | extract.fcntl.LOCK_NB)
                    with mock.patch.object(subprocess, "run") as build:
                        with self.assertRaises(BlockingIOError):
                            extract.main()
                        build.assert_not_called()

    def test_unsupported_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            resources = root / "src/main/resources"
            resources.mkdir(parents=True)
            config = resources / "application.properties"
            for raw in ("a=one\na=two\n", "a:secret", "a=secret\\\n", "a\\b=value"):
                config.write_text(raw, encoding="utf-8")
                with self.assertRaises(ValueError):
                    extract.resource_items(root, "config")
            config.write_text("a=value\n", encoding="utf-8")
            (resources / "application.yml").write_text("a: secret", encoding="utf-8")
            with self.assertRaises(ValueError):
                extract.resource_items(root, "config")


if __name__ == "__main__":
    unittest.main()
