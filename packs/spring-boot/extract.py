#!/usr/bin/env python3
"""Bounded adapter for the public example; no network services or environment export."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

PROFILES = ("contract", "extra")
KINDS = ("api", "routes", "migrations", "config")
LIMIT = 1 << 20


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON field")
        result[key] = value
    return result


def invalid_number(_):
    raise ValueError("invalid number")


def document(path):
    with path.open("rb") as source:
        data = source.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("export exceeds output limit")
    doc = json.loads(data, object_pairs_hook=unique, parse_constant=invalid_number)
    if not isinstance(doc, dict) or set(doc) != {"schema", "items"} or type(doc["schema"]) is not int or doc["schema"] != 1 or not isinstance(doc["items"], list):
        raise ValueError("invalid export envelope")
    seen = set()
    for item in doc["items"]:
        if not isinstance(item, dict) or set(item) != {"id", "value"} or not isinstance(item["id"], str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}", item["id"]) or not isinstance(item["value"], dict) or item["id"] in seen:
            raise ValueError("invalid or duplicate export item")
        seen.add(item["id"])
    return doc["items"]


def exports(directory, kind):
    result = []
    for profile in PROFILES:
        items = document(directory / (profile + "-" + kind + ".json"))
        if not items:
            raise ValueError("empty profile export; investigate coverage before changing the adapter")
        for item in items:
            if item["value"].get("profile") != profile or not item["id"].startswith(profile + ":"):
                raise ValueError("profile identity mismatch")
        result.extend(items)
    return result


def resource_items(project, kind):
    resources = project / "src/main/resources"
    items = []
    if kind == "migrations":
        paths = sorted((resources / "db/migration").glob("*.sql"))
        if not paths:
            raise ValueError("no migration files; explicit adapter review required")
        for path in paths:
            data = bounded_read(path)
            items.append({"id": path.name, "value": {"sha256": hashlib.sha256(data).hexdigest()}})
    else:
        paths = sorted(resources.glob("application*.properties"))
        if not paths or not (resources / "application.properties").is_file():
            raise ValueError("missing application.properties")
        if list(resources.glob("application*.yml")) or list(resources.glob("application*.yaml")):
            raise ValueError("YAML configuration is outside this pack's supported format")
        for path in paths:
            seen = set()
            for line in bounded_read(path).decode("utf-8").splitlines():
                line = line.strip()
                if not line or line.startswith(("#", "!")):
                    continue
                # Deliberately reject escapes/continuations and non-simple properties syntax.
                match = re.fullmatch(r"([A-Za-z0-9_.-]+)\s*=(.*)", line)
                if not match or "\\" in line:
                    raise ValueError("unsupported properties syntax; values were not exported")
                key = match.group(1)
                if key in seen:
                    raise ValueError("duplicate configuration key")
                seen.add(key)
                items.append({"id": path.name + ":" + key, "value": {"source": path.name, "key": key}})
    return items


def bounded_read(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError("expected a regular resource file")
    with path.open("rb") as source:
        data = source.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("resource exceeds size limit")
    return data


def emit(items):
    if any(not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._:/+\-]{0,127}", item["id"]) for item in items):
        raise ValueError("item identifier outside surface contract")
    data = json.dumps({"schema": 1, "items": sorted(items, key=lambda item: item["id"])}, separators=(",", ":"), ensure_ascii=True, allow_nan=False)
    if len(data.encode()) > LIMIT:
        raise ValueError("combined output exceeds limit")
    print(data)


def main():
    if len(sys.argv) != 2 or sys.argv[1] not in KINDS:
        raise ValueError("usage: extract.py api|routes|migrations|config")
    kind = sys.argv[1]
    root = Path(os.environ.get("NARU_ROOT", ".")).resolve()
    project = root / "examples/spring-boot"
    if kind in ("config", "migrations"):
        emit(resource_items(project, kind))
        return
    state = root / ".naru/state"
    state.mkdir(parents=True, exist_ok=True)
    # All surfaces share this build lock; each run owns a fresh export directory.
    with (state / "spring-boot-build.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with tempfile.TemporaryDirectory(prefix="spring-contract-", dir=state) as output:
            subprocess.run(["mvn", "-B", "-q", "-f", str(project / "pom.xml"), "clean", "test", "-Dtest=ContractExportTest,ExtraContractExportTest", "-Dnaru.output=" + output], check=True, stdout=sys.stderr, stderr=sys.stderr, timeout=240)
            emit(exports(Path(output), kind))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError):
        # Do not echo malformed configuration contents, command output or secret values.
        print("spring-boot extractor failed; inspect local build diagnostics and supported input formats", file=sys.stderr)
        sys.exit(1)
