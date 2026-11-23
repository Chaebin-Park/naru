"""Build and package five unpublished, source-bound release archives."""

import argparse
import hashlib
from pathlib import Path
import re
import stat
import subprocess
import tarfile
import tempfile
import zipfile

PLATFORMS = (
    ("darwin", "amd64"),
    ("darwin", "arm64"),
    ("linux", "amd64"),
    ("linux", "arm64"),
    ("windows", "amd64"),
)
METADATA = ("LICENSE", "REVISION", "VERSION", "TOOLCHAIN", "BUILD_SETTINGS")
CHECKSUMS = "SHA256SUMS"


def binary_name(system, architecture):
    extension = ".exe" if system == "windows" else ""
    return f"naru-{system}-{architecture}{extension}"


def checksums(directory, names):
    return "".join(
        f"{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n"
        for name in sorted(names)
    )


def validate_candidate(candidate, revision, version):
    names = set(METADATA) | {binary_name(*platform) for platform in PLATFORMS}
    if {entry.name for entry in candidate.iterdir()} != names | {CHECKSUMS}:
        raise ValueError("candidate must contain exactly the expected binaries and metadata")
    for name in names | {CHECKSUMS}:
        info = (candidate / name).lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not info.st_size:
            raise ValueError(f"candidate entry must be a nonempty regular file: {name}")
    if (candidate / "REVISION").read_text(encoding="ascii") != revision + "\n":
        raise ValueError("candidate revision changed during build")
    if (candidate / "VERSION").read_text(encoding="ascii") != version + "\n":
        raise ValueError("candidate version does not match the requested version")
    if (candidate / CHECKSUMS).read_text(encoding="ascii") != checksums(candidate, names):
        raise ValueError("candidate checksum manifest does not match candidate files")


def archive_identity(info):
    info.uid = info.gid = 0
    info.uname = info.gname = ""
    return info


def package(candidate, output, revision, version):
    validate_candidate(candidate, revision, version)
    output.mkdir()  # Exclusive even if another invocation built the same release.
    archive_names = []
    for system, architecture in PLATFORMS:
        base = f"naru_{version[1:]}_{system}_{architecture}"
        binary = candidate / binary_name(system, architecture)
        members = [(candidate / name, name) for name in METADATA]
        if system == "windows":
            archive = output / f"{base}.zip"
            with zipfile.ZipFile(archive, "x", compression=zipfile.ZIP_DEFLATED) as bundle:
                for source, name in [(binary, "naru.exe"), *members]:
                    bundle.write(source, name)
        else:
            archive = output / f"{base}.tar.gz"
            with archive.open("xb") as stream:
                with tarfile.open(fileobj=stream, mode="w:gz") as bundle:
                    for source, name in [(binary, "naru"), *members]:
                        bundle.add(source, arcname=name, recursive=False, filter=archive_identity)
        archive_names.append(archive.name)
    with (output / CHECKSUMS).open("x", encoding="ascii", newline="\n") as manifest:
        manifest.write(checksums(output, archive_names))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="new absolute directory outside the checkout")
    parser.add_argument("version", help="explicit vMAJOR.MINOR.PATCH version")
    args = parser.parse_args()
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", args.version):
        parser.error("release version must be vMAJOR.MINOR.PATCH")
    if not args.output.is_absolute() or ".." in args.output.parts:
        parser.error("output must be an absolute path without parent traversal")

    try:
        root = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip()).resolve()
        output = args.output.parent.resolve(strict=True) / args.output.name
        if output == root or root in output.parents:
            parser.error("output must be outside the source checkout")
        if output.exists() or output.is_symlink():
            parser.error("output already exists")
        revision = subprocess.check_output(
            ["git", "--no-replace-objects", "rev-parse", "HEAD"], cwd=root, text=True
        ).strip()
        with tempfile.TemporaryDirectory(prefix="naru-release-") as temporary:
            candidate = Path(temporary) / "candidate"
            prepare = Path(__file__).resolve().with_name("prepare-candidate.sh")
            subprocess.run(
                ["sh", str(prepare), str(candidate), args.version], cwd=root, check=True
            )
            package(candidate, output, revision, args.version)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"release packaging failed: {error}\n")
    print(f"Unpublished release archives: {output}")


if __name__ == "__main__":
    main()
