#!/usr/bin/env python3
"""Publish immutable CLI assets, then promote a single version pointer."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request

PREFIX = "builds/releases"
BINARIES = (
    "volcano-linux-amd64", "volcano-linux-arm64", "volcano-macos-amd64",
    "volcano-macos-arm64", "volcano-windows-amd64.exe",
)
SIGNED = (*BINARIES, "install.sh")
ASSETS = (*SIGNED, *(name + ".sigstore.json" for name in SIGNED), "SHA256SUMS")
IMMUTABLE = "public,max-age=31536000,immutable"
MUTABLE = "public,max-age=60"


def version_tuple(version):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError(f"invalid release version: {version!r}")
    return tuple(map(int, version[1:].split(".")))


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def aws(*args):
    result = subprocess.run(
        ["aws", "s3api", *args, "--output", "json"],
        capture_output=True, text=True, check=False,
    )
    if result.returncode:
        # Only a missing key is absence; permissions and network failures stop promotion.
        if args[0] == "get-object" and "(NoSuchKey)" in result.stderr:
            return None
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout or "{}")


def put(bucket, key, path, cache, **conditions):
    args = ["put-object", "--bucket", bucket, "--key", key, "--body", str(path),
            "--cache-control", cache, "--content-type", "application/octet-stream"]
    for name, value in conditions.items():
        args.extend(["--" + name.replace("_", "-"), value])
    return aws(*args)


def verify_signatures(assets, version):
    identity = "https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml@refs/tags/" + version
    for name in SIGNED:
        subprocess.run([
            "cosign", "verify-blob", str(assets / name),
            "--bundle", str(assets / (name + ".sigstore.json")),
            "--certificate-identity", identity,
            "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
        ], check=True)


def verify_downloads(base_url, version, assets):
    for name in ASSETS:
        url = f"{base_url}/download/{version}/{name}"
        for attempt in range(3):
            try:
                with urllib.request.urlopen(url, timeout=30) as response:
                    actual = hashlib.file_digest(response, "sha256").hexdigest()
                if actual != digest(assets / name):
                    raise ValueError(f"download checksum mismatch: {url}")
                break
            except (OSError, ValueError):
                if attempt == 2:
                    raise
                time.sleep(5 * (attempt + 1))


def verify_install(assets, version, base_url):
    with tempfile.TemporaryDirectory(prefix="cli-install-") as directory:
        env = dict(os.environ, VOLCANO_VERSION=version, VOLCANO_DOWNLOAD_URL=base_url,
                   VOLCANO_INSTALL_DIR=directory)
        env.pop("VOLCANO_GITHUB_RELEASES_URL", None)
        env.pop("VOLCANO_SKIP_SIGNATURE_VERIFICATION", None)
        subprocess.run(["sh", str(assets / "install.sh")], env=env, check=True, timeout=600)
        result = subprocess.run([str(Path(directory) / "volcano"), "--version"],
                                capture_output=True, text=True, check=True, timeout=30)
        if not re.search(r"(?<![0-9A-Za-z.])" + re.escape(version) + r"(?![0-9A-Za-z.])", result.stdout):
            raise ValueError(f"installed CLI did not report {version}: {result.stdout}")


def publish(assets, version, bucket, base_url):
    candidate = version_tuple(version)
    for name in ASSETS:
        if not (assets / name).is_file() or (assets / name).stat().st_size == 0:
            raise ValueError(f"missing release asset: {name}")
    verify_signatures(assets, version)
    with tempfile.TemporaryDirectory(prefix="cli-publish-") as work:
        work = Path(work)
        current = work / "latest-version"
        metadata = aws("get-object", "--bucket", bucket, "--key", f"{PREFIX}/latest-version", str(current))
        promote = metadata is None or candidate >= version_tuple(current.read_text().strip())
        # Reuse existing bundles on retries: keyless signing produces different bytes.
        # Signed files themselves may never change for an already-published version.
        for name in (*SIGNED, *(name + ".sigstore.json" for name in SIGNED)):
            key = f"{PREFIX}/download/{version}/{name}"
            existing = work / name
            found = aws("get-object", "--bucket", bucket, "--key", key, str(existing))
            if found is not None:
                if name in SIGNED and digest(existing) != digest(assets / name):
                    raise ValueError(f"refusing to overwrite immutable asset: {key}")
                (assets / name).write_bytes(existing.read_bytes())
            else:
                put(bucket, key, assets / name, IMMUTABLE, if_none_match="*")
        verify_signatures(assets, version)
        checksum_names = sorted(name for name in ASSETS if name != "SHA256SUMS")
        (assets / "SHA256SUMS").write_text("".join(f"{digest(assets / name)}  {name}\n" for name in checksum_names))
        key = f"{PREFIX}/download/{version}/SHA256SUMS"
        existing = work / "SHA256SUMS"
        if aws("get-object", "--bucket", bucket, "--key", key, str(existing)) is None:
            put(bucket, key, assets / "SHA256SUMS", IMMUTABLE, if_none_match="*")
        elif digest(existing) != digest(assets / "SHA256SUMS"):
            raise ValueError(f"refusing to overwrite immutable asset: {key}")
        verify_downloads(base_url, version, assets)
        verify_install(assets, version, base_url)
        if not promote:
            print(f"Published {version}; retained newer latest version")
            return
        # This bootstrap resolves latest-version; it must work with the old pointer too.
        put(bucket, "builds/install.sh", assets / "install.sh", MUTABLE)
        current.write_text(version + "\n")
        condition = {"if_match": metadata["ETag"]} if metadata else {"if_none_match": "*"}
        put(bucket, f"{PREFIX}/latest-version", current, MUTABLE, **condition)
        print(f"Promoted {version} at {base_url}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--bucket", required=True)
    parser.add_argument("--base-url", default="https://download.volcano.dev/builds/releases")
    args = parser.parse_args()
    publish(args.assets, args.version, args.bucket, args.base_url.rstrip("/"))


if __name__ == "__main__":
    main()
