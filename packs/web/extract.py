#!/usr/bin/env python3
"""Strict adapter for the public local-workspace Node sample; no registry resolver."""
import fcntl
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unicodedata

LIMIT = 1 << 20
TIMEOUT = 60
ITEM_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}\Z")
ROOT_FILES = ("package.json", "package-lock.json", ".env.example")
REQUIRED = (*ROOT_FILES, "src/routes.mjs", "src/server.mjs", "scripts/build.mjs", "test/runtime.test.mjs", "packages/message/package.json", "packages/message/index.mjs")
METHODS = {"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
VERSIONS = {"node": "22.22.2", "npm": "10.9.7"}


def shape(value, required, optional=()):
    if type(value) is not dict or not set(required) <= set(value) or set(value) - set(required) - set(optional):
        raise ValueError("unsupported object fields")


def unique(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON field")
        value[key] = item
    return value


def bad_number(_):
    raise ValueError("nonfinite JSON number")


def read(path, root, limit=LIMIT):
    path, root = Path(path), Path(root)
    path.relative_to(root)
    for entry in (path, *path.parents):
        mode = entry.lstat().st_mode
        if stat.S_ISLNK(mode) or (entry == path and not stat.S_ISREG(mode)):
            raise ValueError("unsupported source file type")
        if entry == root:
            break
    with path.open("rb") as source:
        data = source.read(limit + 1)
    if len(data) > limit:
        raise ValueError("input exceeds size limit")
    return data


def document(path, root):
    try:
        return json.loads(read(path, root).decode("utf-8"), object_pairs_hook=unique, parse_constant=bad_number)
    except (UnicodeError, json.JSONDecodeError) as error:
        raise ValueError("invalid JSON encoding or syntax") from error


def output(items):
    seen = set()
    for item in items:
        if not ITEM_ID.fullmatch(item["id"]) or item["id"] in seen:
            raise ValueError("unsupported or duplicate item identity")
        seen.add(item["id"])
    result = {"schema": 1, "items": sorted(items, key=lambda item: item["id"])}
    if len(json.dumps(result, sort_keys=True, separators=(",", ":")).encode()) > LIMIT:
        raise ValueError("output exceeds size limit")
    return result


def validate_routes(directory, nonce):
    directory = Path(directory)
    if directory.is_symlink() or {path.name for path in directory.iterdir()} != {"web-export.json"}:
        raise ValueError("missing or unexpected export")
    value = document(directory / "web-export.json", directory)
    shape(value, ("schema", "sample", "request_id", "tools", "routes"))
    shape(value["tools"], ("node",))
    if type(value["schema"]) is not int or value["schema"] != 1 or value["sample"] != "naru-web" or value["request_id"] != nonce or not re.fullmatch(r"[0-9a-f]{32}", nonce) or value["tools"]["node"] != VERSIONS["node"] or type(value["routes"]) is not list or len(value["routes"]) > 1024:
        raise ValueError("invalid export envelope")
    items = []
    for route in value["routes"]:
        shape(route, ("method", "path"))
        method, path = route["method"], route["path"]
        if type(method) is not str or method not in METHODS or type(path) is not str or not re.fullmatch(r"/(?:[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)?", path) or any(part in (".", "..") for part in path.split("/")):
            raise ValueError("unsupported route")
        items.append({"id": method + ":" + path, "value": route})
    return output(items)


def version(value):
    if type(value) is not str or len(value) > 128 or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?(?:\+[A-Za-z0-9.-]+)?", value):
        raise ValueError("unsupported package version")


def metadata(value):
    if "description" in value and (type(value["description"]) is not str or len(value["description"]) > 1024):
        raise ValueError("unsupported description metadata")
    if "license" in value and (type(value["license"]) is not str or len(value["license"]) > 256):
        raise ValueError("unsupported license metadata")
    if "engines" in value:
        shape(value["engines"], (), ("node", "npm"))
        if any(type(item) is not str or len(item) > 256 for item in value["engines"].values()):
            raise ValueError("unsupported engine metadata")


def dependencies(root):
    root = Path(root)
    package = document(root / "package.json", root)
    workspace = document(root / "packages/message/package.json", root)
    lock = document(root / "package-lock.json", root)
    shape(package, ("name", "version", "private", "type", "workspaces", "dependencies"), ("engines", "license", "description"))
    shape(workspace, ("name", "version", "private", "type", "exports"), ("engines", "license", "description"))
    if package["name"] != "naru-web-example" or package["private"] is not True or package["type"] != "module" or package["workspaces"] != ["packages/message"] or package["dependencies"] != {"naru-web-message": "*"} or workspace["name"] != "naru-web-message" or workspace["private"] is not True or workspace["type"] != "module" or workspace["exports"] != "./index.mjs":
        raise ValueError("unsupported local workspace declaration")
    for item in (package, workspace):
        version(item["version"])
        metadata(item)
    shape(lock, ("name", "version", "lockfileVersion", "requires", "packages"))
    if type(lock["lockfileVersion"]) is not int or lock["lockfileVersion"] != 3 or lock["requires"] is not True or lock["name"] != package["name"] or lock["version"] != package["version"]:
        raise ValueError("unsupported lock header")
    shape(lock["packages"], ("", "packages/message", "node_modules/naru-web-message"))
    first, linked, local = lock["packages"][""], lock["packages"]["node_modules/naru-web-message"], lock["packages"]["packages/message"]
    shape(first, ("name", "version", "workspaces", "dependencies"), ("engines", "license"))
    shape(local, ("version",), ("name", "engines", "license"))
    shape(linked, ("resolved", "link"))
    if linked["link"] is not True or linked["resolved"] != "packages/message":
        raise ValueError("unsupported workspace link")
    for record, manifest in ((first, package), (local, workspace)):
        metadata(record)
        for key, value in record.items():
            if value != manifest.get(key):
                raise ValueError("manifest and lock metadata differ")
    return output([
        {"id": "root", "value": {"kind": "root", "name": package["name"], "version": package["version"], "dependencies": ["workspace:naru-web-message"]}},
        {"id": "workspace:naru-web-message", "value": {"kind": "workspace", "name": workspace["name"], "version": workspace["version"], "path": "packages/message", "dependencies": []}},
    ])


def environment(path):
    path = Path(path)
    try:
        source = read(path, path.parent, 65536).decode("utf-8")
    except UnicodeError as error:
        raise ValueError("invalid environment encoding") from error
    if re.search(r"\r(?!\n)", source) or "\u2028" in source or "\u2029" in source or any(unicodedata.category(char) == "Cc" and char not in "\r\n" for char in source):
        raise ValueError("invalid environment control character")
    names = set()
    for line in source.splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,123}", key) or key in names or value.endswith("\\") or (value.startswith(("'", '"')) and (len(value) < 2 or value[-1] != value[0])):
            raise ValueError("invalid environment key assignment")
        names.add(key)
        if len(names) > 1024:
            raise ValueError("too many environment keys")
    return output([{"id": "env:" + key, "value": {"name": key}} for key in names])


def copy_sample(root, target):
    source = root / "examples/web"
    for directory in (root / "examples", source):
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError("unsupported sample directory")
    found, count, size = set(), 0, 0
    for directory, dirs, files in os.walk(source, followlinks=False):
        relative = Path(directory).relative_to(source)
        for name in list(dirs):
            entry = relative / name
            if entry.as_posix() in ("node_modules", "dist"):
                dirs.remove(name)
                continue
            if entry.as_posix() not in ("scripts", "src", "test", "packages", "packages/message") or (Path(directory) / name).is_symlink():
                raise ValueError("unsupported source directory")
        for name in files:
            entry = relative / name
            name = entry.as_posix()
            if name in ("README.md", ".npmrc", ".env"):
                continue
            allowed = name in ROOT_FILES or name == "packages/message/package.json" or (entry.parent.as_posix() in ("scripts", "src", "test", "packages/message") and entry.suffix == ".mjs")
            if not allowed:
                raise ValueError("unsupported source path")
            count += 1
            if count > 1024:
                raise ValueError("too many sample files")
            data = read(source / entry, root)
            size += len(data)
            if size > 8 * LIMIT:
                raise ValueError("sample exceeds copy limit")
            destination = target / entry
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
            found.add(name)
    if not set(REQUIRED) <= found:
        raise ValueError("missing sample source")


def group_exists(pid):
    try:
        os.killpg(pid, 0)
        return True
    except ProcessLookupError:
        return False


def stop_group(process):
    for sig in (signal.SIGTERM, signal.SIGKILL):
        if not group_exists(process.pid):
            break
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            break
        try:
            process.wait(timeout=1)
        except subprocess.TimeoutExpired:
            pass
        deadline = time.monotonic() + 1
        while group_exists(process.pid) and time.monotonic() < deadline:
            time.sleep(0.02)
    if process.poll() is None or group_exists(process.pid):
        raise RuntimeError("web child cleanup unresolved")


def run_step(command, cwd, env, deadline, step):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise RuntimeError("web pipeline deadline exceeded")
    with tempfile.TemporaryFile() as logs:
        process = subprocess.Popen(command, cwd=cwd, env=env, stdout=logs, stderr=logs, start_new_session=True)
        try:
            code = process.wait(timeout=remaining)
        finally:
            stop_group(process)
        if code != 0:
            raise RuntimeError("web " + step + " failed")
        logs.seek(0)
        # Only version probes consume output; raw diagnostic content is never echoed.
        return logs.read(256)


def build(root, timeout=TIMEOUT):
    root = Path(root).resolve()
    state = root / ".naru/state"
    for path in (root / ".naru", state):
        if path.is_symlink():
            raise ValueError("unsupported state directory")
        path.mkdir(exist_ok=True)
    fd = os.open(state / "web-build.lock", os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    with os.fdopen(fd, "w") as lock:
        if not stat.S_ISREG(os.fstat(lock.fileno()).st_mode):
            raise ValueError("unsupported build lock")
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix="naru-web-") as temporary:
            temporary = Path(temporary)
            sample, exports = temporary / "sample", temporary / "exports"
            home, tmp, cache = temporary / "home", temporary / "tmp", temporary / "cache"
            for path in (sample, exports, home, tmp, cache):
                path.mkdir()
            userconfig, globalconfig = temporary / "user.npmrc", temporary / "global.npmrc"
            userconfig.write_text("")
            globalconfig.write_text("")
            copy_sample(root, sample)
            dep, keys = dependencies(sample), environment(sample / ".env.example")
            env = {"PATH": os.environ.get("PATH", os.defpath), "HOME": str(home), "TMPDIR": str(tmp), "LANG": "C", "LC_ALL": "C"}
            npm = ["npm", "--userconfig=" + str(userconfig), "--globalconfig=" + str(globalconfig), "--cache=" + str(cache)]
            deadline = time.monotonic() + timeout
            if run_step(["node", "--version"], sample, env, deadline, "node version").strip() != b"v22.22.2":
                raise ValueError("unsupported Node version")
            if run_step(npm + ["--version"], sample, env, deadline, "npm version").strip() != b"10.9.7":
                raise ValueError("unsupported npm version")
            run_step(npm + ["ci", "--offline", "--ignore-scripts", "--no-audit", "--no-fund"], sample, env, deadline, "offline workspace preparation")
            run_step(["node", "--test", "test/runtime.test.mjs"], sample, env, deadline, "runtime tests")
            nonce = secrets.token_hex(16)
            run_step(["node", "scripts/build.mjs", str(exports), nonce], sample, env, deadline, "export")
            routes = validate_routes(exports, nonce)
            # Detect build code mutating the lock/env before reporting their surface.
            if dep != dependencies(sample) or keys != environment(sample / ".env.example"):
                raise ValueError("build changed contract inputs")
            return {"routes": routes, "dependencies": dep, "env": keys}


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ("routes", "dependencies", "env", "contracts"):
        raise ValueError("usage: extract.py routes|dependencies|env|contracts")
    value = build(Path(os.environ.get("NARU_ROOT", Path.cwd())))
    if sys.argv[1] == "contracts":
        print("web contracts passed", file=sys.stderr)
    else:
        print(json.dumps(value[sys.argv[1]], sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    def interrupt(_signal, _frame):
        raise InterruptedError("web extraction interrupted")
    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    try:
        main()
    except (ValueError, OSError, RuntimeError, subprocess.TimeoutExpired):
        # Even exception text may contain a secret-bearing filename/line or child argv.
        print("web adapter: extraction failed; check the declared sample contract and tools", file=sys.stderr)
        sys.exit(1)
