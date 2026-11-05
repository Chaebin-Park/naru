#!/usr/bin/env python3
"""Fresh-build adapter for the bounded public Android sample; standard library only."""
import fcntl
import hashlib
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
import xml.etree.ElementTree as ET

LIMIT = 1 << 20
BUILD_TIMEOUT = 240
ANDROID = "{http://schemas.android.com/apk/res/android}"
ITEM_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}\Z")
ROOT_FILES = ("settings.gradle", "build.gradle", "gradle.properties", "naru-export.gradle", "app/build.gradle", "library/build.gradle")
TOOLS = {"gradle": "8.11.1", "agp": "8.9.2", "java_major": 17, "compile_sdk": 35, "build_tools": "35.0.0"}


def exact(value, keys):
    if type(value) is not dict or set(value) != set(keys):
        raise ValueError("missing or unknown object fields")


def text(value):
    if type(value) is not str or not value or len(value.encode("utf-8")) > 256 or any(unicodedata.category(c) == "Cc" for c in value) or "${" in value:
        raise ValueError("invalid identity or unresolved placeholder")
    return value


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON field")
        result[key] = value
    return result


def invalid_number(_):
    raise ValueError("nonfinite JSON number")


def regular(path, root):
    path, root = Path(path), Path(root)
    path.relative_to(root)
    for entry in [path, *path.parents]:
        mode = entry.lstat().st_mode
        if stat.S_ISLNK(mode) or (entry == path and not stat.S_ISREG(mode)):
            raise ValueError("symlink or nonregular file")
        if entry == root:
            break
    with path.open("rb") as source:
        data = source.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("file exceeds 1 MiB")
    return data


def output(items):
    seen = set()
    for item in items:
        if not ITEM_ID.fullmatch(item["id"]) or item["id"] in seen:
            raise ValueError("unsupported or duplicate surface identity")
        seen.add(item["id"])
    value = {"schema": 1, "items": sorted(items, key=lambda item: item["id"])}
    if len(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()) > LIMIT:
        raise ValueError("normalized output exceeds 1 MiB")
    return value


def component(value):
    exact(value, ("id", "kind", "project_path", "module", "module_origin"))
    exact(value["module"], ("group", "name", "version"))
    module = value["module"]
    for scalar in module.values():
        text(scalar)
    if module["version"] == "unspecified":
        raise ValueError("missing selected version")
    if value["kind"] == "project":
        path = text(value["project_path"])
        if not re.fullmatch(r":[A-Za-z0-9_.-]+(?::[A-Za-z0-9_.-]+)*", path) or value["id"] != "project:" + path or value["module_origin"] not in ("resolution", "declared-project"):
            raise ValueError("unsupported project component")
    elif value["kind"] == "module":
        if value["project_path"] is not None or value["module_origin"] != "resolution" or value["id"] != "module:" + module["group"] + ":" + module["name"]:
            raise ValueError("unsupported module component")
    else:
        raise ValueError("unsupported component kind")
    text(value["id"])
    return value


def dependencies(value):
    exact(value, ("configuration", "root", "components", "edges"))
    if value["configuration"] != "debugRuntimeClasspath" or type(value["components"]) is not list or type(value["edges"]) is not list or len(value["components"]) > 4096 or len(value["edges"]) > 16384:
        raise ValueError("invalid dependency graph")
    root = component(value["root"])
    if root["kind"] != "project" or root["project_path"] != ":app":
        raise ValueError("wrong graph root")
    nodes = {root["id"]: root}
    for node in value["components"]:
        node = component(node)
        if node["id"] in nodes:
            raise ValueError("duplicate component")
        nodes[node["id"]] = node
    adjacency = {key: set() for key in nodes}
    for edge in value["edges"]:
        exact(edge, ("from", "to"))
        start, end = text(edge["from"]), text(edge["to"])
        if start not in nodes or end not in nodes or end in adjacency[start]:
            raise ValueError("dangling or duplicate edge")
        adjacency[start].add(end)
    visited, pending = set(), [root["id"]]
    while pending:
        key = pending.pop()
        if key not in visited:
            visited.add(key)
            pending.extend(adjacency[key])
    if visited != set(nodes):
        raise ValueError("unreachable component")
    items = []
    for key, node in nodes.items():
        is_root = key == root["id"]
        item = {"variant": "debug", "configuration": value["configuration"], "kind": "root" if is_root else node["kind"], "project_path": node["project_path"], "module": node["module"], "module_origin": node["module_origin"], "dependencies": sorted(adjacency[key])}
        items.append({"id": "debug:debugRuntimeClasspath:" + ("root" if is_root else key), "value": item})
    return output(items)


def manifest(data):
    raw = data.decode("utf-8")
    if re.search(r"<!\s*(?:DOCTYPE|ENTITY)", raw, re.I):
        raise ValueError("DTD and entities are unsupported")
    tree = ET.fromstring(raw)
    if tree.tag != "manifest":
        raise ValueError("wrong XML root")
    package = text(tree.get("package"))
    if not re.fullmatch(r"[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)+", package):
        raise ValueError("invalid manifest package")

    def name(value):
        value = text(value)
        result = package + value if value.startswith(".") else package + "." + value if "." not in value else value
        if not re.fullmatch(r"[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)+", result):
            raise ValueError("invalid component name")
        return result

    def permission(element, key="permission", fallback=None):
        value = element.get(ANDROID + key)
        return text(value) if value is not None else fallback

    items, applications = [], tree.findall("application")
    if len(applications) != 1:
        raise ValueError("expected one application")
    for child in tree:
        if child.tag.startswith("uses-permission") and child.tag != "uses-permission":
            raise ValueError("unsupported permission form")
        if child.tag == "uses-permission":
            perm = text(child.get(ANDROID + "name"))
            maximum = child.get(ANDROID + "maxSdkVersion")
            if maximum is not None and (not re.fullmatch(r"[1-9][0-9]*", maximum) or len(maximum) > 9):
                raise ValueError("invalid maxSdkVersion")
            items.append({"id": "debug:permission:" + perm, "value": {"variant": "debug", "name": perm, "max_sdk_version": int(maximum) if maximum else None}})
    app = applications[0]
    inherited = permission(app)
    for child in app:
        if child.tag not in ("activity", "activity-alias", "service", "receiver", "provider"):
            continue
        identity = name(child.get(ANDROID + "name"))
        exported = child.get(ANDROID + "exported")
        if exported not in ("true", "false"):
            raise ValueError("explicit exported boolean required")
        value = {"variant": "debug", "kind": child.tag, "name": identity, "exported": exported == "true", "permission": permission(child, fallback=inherited)}
        if child.tag == "activity-alias":
            value["target_activity"] = name(child.get(ANDROID + "targetActivity"))
        if child.tag == "provider":
            authorities = text(child.get(ANDROID + "authorities")).split(";")
            for authority in authorities:
                text(authority)
                if not re.fullmatch(r"[A-Za-z0-9_.-]+", authority):
                    raise ValueError("invalid authority")
            if len(set(authorities)) != len(authorities):
                raise ValueError("duplicate authority")
            value.update(authorities=sorted(authorities), read_permission=permission(child, "readPermission"), write_permission=permission(child, "writePermission"))
        items.append({"id": "debug:" + child.tag + ":" + identity, "value": value})
    return output(items)


def validate_exports(directory, request_id):
    directory = Path(directory)
    if directory.is_symlink() or {path.name for path in directory.iterdir()} != {"debug-export.json", "debug-merged-manifest.xml"}:
        raise ValueError("unexpected export directory contents")
    data = regular(directory / "debug-export.json", directory)
    value = json.loads(data.decode("utf-8"), object_pairs_hook=unique, parse_constant=invalid_number)
    exact(value, ("schema", "sample", "variant", "request_id", "tools", "manifest", "dependencies"))
    if type(value["schema"]) is not int or value["schema"] != 1 or value["sample"] != "naru-android" or value["variant"] != "debug" or not re.fullmatch(r"[0-9a-f]{32}", request_id) or value["request_id"] != request_id:
        raise ValueError("wrong schema, sample, variant or request")
    exact(value["tools"], TOOLS)
    if any(type(value["tools"][key]) is not type(expected) or value["tools"][key] != expected for key, expected in TOOLS.items()):
        raise ValueError("unsupported tool versions")
    exact(value["manifest"], ("file", "sha256"))
    xml = regular(directory / "debug-merged-manifest.xml", directory)
    if value["manifest"]["file"] != "debug-merged-manifest.xml" or value["manifest"]["sha256"] != "sha256:" + hashlib.sha256(xml).hexdigest():
        raise ValueError("manifest identity or digest mismatch")
    return {"manifest": manifest(xml), "dependencies": dependencies(value["dependencies"])}


def copy_sample(root, destination):
    source = root / "examples/android"
    for directory in (root, root / "examples", source):
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError("missing or symlink source directory")
    paths = [source / name for name in ROOT_FILES]
    for module in ("app", "library"):
        directory = source / module / "src"
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError("missing or symlink source directory")
        for current, dirs, files in os.walk(directory, followlinks=False):
            for name in dirs:
                if (Path(current) / name).is_symlink():
                    raise ValueError("symlink source directory")
            for name in files:
                paths.append(Path(current) / name)
                if len(paths) > 4096:
                    raise ValueError("too many source files")
    if len(paths) > 4096:
        raise ValueError("too many source files")
    total = 0
    for path in paths:
        data = regular(path, source)
        total += len(data)
        if total > 16 * LIMIT:
            raise ValueError("sample exceeds copy limit")
        target = destination / path.relative_to(source)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)


def group_exists(pid):
    try:
        os.killpg(pid, 0)
        return True
    except ProcessLookupError:
        return False


def stop_group(process):
    # Only the group created by this invocation is addressed, never a name match.
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
        raise RuntimeError("build process cleanup unresolved")


def sdk_preflight():
    selected = os.environ.get("ANDROID_HOME")
    if not selected:
        raise ValueError("ANDROID_HOME must select an installed Android SDK")
    sdk = Path(selected)
    for relative in ("platforms/android-35/android.jar", "build-tools/35.0.0/aapt2", "build-tools/35.0.0/source.properties"):
        path = sdk / relative
        if not path.is_file() or path.stat().st_size == 0:
            raise ValueError("Android SDK platform 35 and Build Tools 35.0.0 must already be installed")
    with (sdk / "build-tools/35.0.0/source.properties").open("rb") as source:
        properties = source.read(LIMIT + 1)
    if len(properties) > LIMIT or not re.search(rb"(?m)^Pkg\.Revision\s*=\s*35\.0\.0\s*$", properties):
        raise ValueError("unsupported installed Build Tools revision")


def build(root, timeout=BUILD_TIMEOUT):
    root = Path(root).resolve()
    sdk_preflight()
    state = root / ".naru/state"
    for directory in (root / ".naru", state):
        if directory.is_symlink():
            raise ValueError("symlink state directory")
        directory.mkdir(exist_ok=True)
    lockpath = state / "android-build.lock"
    fd = os.open(lockpath, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    with os.fdopen(fd, "w") as lock:
        if not stat.S_ISREG(os.fstat(lock.fileno()).st_mode):
            raise ValueError("nonregular lock")
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix="naru-android-") as temporary:
            sample, exports = Path(temporary) / "sample", Path(temporary) / "exports"
            sample.mkdir()
            exports.mkdir()
            copy_sample(root, sample)
            request_id = secrets.token_hex(16)
            command = ["gradle", "--no-daemon", "--console=plain", "--stacktrace", "-Pandroid.builder.sdkDownload=false", "-p", str(sample), "-PnaruOutput=" + str(exports), "-PnaruRequestId=" + request_id, "clean", ":app:assembleDebug", ":app:exportNaruDebug"]
            # A file drains build diagnostics without unbounded in-memory buffering.
            with tempfile.TemporaryFile() as logs:
                process = subprocess.Popen(command, stdout=logs, stderr=logs, start_new_session=True)
                try:
                    code = process.wait(timeout=timeout)
                finally:
                    stop_group(process)
                if code:
                    logs.seek(0)
                    detail = logs.read(8192).decode("utf-8", "replace")
                    raise RuntimeError("Gradle build failed: " + detail)
            return validate_exports(exports, request_id)


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in ("manifest", "dependencies", "contracts"):
        raise ValueError("usage: extract.py manifest|dependencies|contracts")
    result = build(Path(os.environ.get("NARU_ROOT", Path.cwd())))
    if sys.argv[1] != "contracts":
        print(json.dumps(result[sys.argv[1]], sort_keys=True, separators=(",", ":")))


if __name__ == "__main__":
    def interrupt(_signal, _frame):
        raise InterruptedError("Android extraction interrupted")
    signal.signal(signal.SIGTERM, interrupt)
    signal.signal(signal.SIGINT, interrupt)
    try:
        main()
    except (ValueError, OSError, RuntimeError, ET.ParseError, subprocess.TimeoutExpired) as error:
        print("android adapter: " + str(error), file=sys.stderr)
        sys.exit(1)
