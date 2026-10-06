#!/usr/bin/env python3
"""Publish immutable CLI assets, then promote the latest alias and version pointer."""

import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import urllib.request

PREFIX = "builds/releases"
BINARIES = (
    "volcano-linux-amd64", "volcano-linux-arm64", "volcano-macos-amd64",
    "volcano-macos-arm64", "volcano-windows-amd64.exe", "volcano-windows-arm64.exe",
)
SIGNED = (*BINARIES, "install.sh")
BUNDLES = tuple(name + ".sigstore.json" for name in SIGNED)
ASSETS = (*SIGNED, *BUNDLES, "SHA256SUMS")
IMMUTABLE = "public,max-age=31536000,immutable"
MUTABLE = "public,max-age=60"


def version_tuple(version):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError(f"invalid release version: {version!r}")
    return tuple(map(int, version[1:].split(".")))


def sha256(stream):
    hasher = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1 << 20), b""):
        hasher.update(chunk)
    return hasher.hexdigest()


def digest(path):
    with path.open("rb") as stream:
        return sha256(stream)


def aws(*args):
    result = subprocess.run(
        ["aws", "s3api", *args, "--output", "json"],
        capture_output=True, text=True, check=False,
    )
    if result.returncode:
        raise RuntimeError(result.stderr)
    return json.loads(result.stdout or "{}")


def listed_keys(bucket, prefix):
    # Without ListBucket authorization for the request, GetObject reports a missing key as AccessDenied.
    listing = aws("list-objects-v2", "--bucket", bucket, "--prefix", prefix)
    return {item["Key"]: item["ETag"] for item in listing.get("Contents") or []}


def get(bucket, key, path, keys):
    return aws("get-object", "--bucket", bucket, "--key", key, str(path)) if key in keys else None


def content_type(key):
    name = key.rsplit("/", 1)[-1]
    if name.endswith(".sigstore.json"):
        return "application/json"
    if name in ("install.sh", "SHA256SUMS", "latest-version"):
        # Readable in a browser, so the installer can be inspected before piping it to sh.
        return "text/plain; charset=utf-8"
    return "application/octet-stream"


def content_headers(key):
    name = key.rsplit("/", 1)[-1]
    headers = ["--content-type", content_type(key)]
    if name in BINARIES:
        # Without it, Safari can save the macOS binary with a .dms suffix.
        headers += ["--content-disposition", f"attachment; filename={name}"]
    return headers


def copy(bucket, source, etag, key):
    # REPLACE drops every source header, so each one is re-specified here.
    return aws("copy-object", "--bucket", bucket, "--key", key,
               "--copy-source", f"{bucket}/{source}", "--copy-source-if-match", etag,
               "--metadata-directive", "REPLACE", "--cache-control", MUTABLE,
               *content_headers(key))


def put(bucket, key, path, cache, **conditions):
    args = ["put-object", "--bucket", bucket, "--key", key, "--body", str(path),
            "--cache-control", cache, *content_headers(key)]
    for name, value in conditions.items():
        args.extend(["--" + name.replace("_", "-"), value])
    return aws(*args)


def verify_signatures(assets, version, signed):
    identity = "https://github.com/Kong/volcano-cli/.github/workflows/publish-cli.yml@refs/tags/" + version
    for name in signed:
        subprocess.run([
            "cosign", "verify-blob", str(assets / name),
            "--bundle", str(assets / (name + ".sigstore.json")),
            "--certificate-identity", identity,
            "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com",
        ], check=True)


def verify_downloads(base_url, version, assets, names=None):
    for name in ASSETS if names is None else names:
        url = f"{base_url}/download/{version}/{name}"
        for attempt in range(3):
            try:
                with urllib.request.urlopen(url, timeout=30) as response:
                    actual = sha256(response)
                if actual != digest(assets / name):
                    raise ValueError(f"download checksum mismatch: {url}")
                break
            except (OSError, ValueError, http.client.HTTPException):
                if attempt == 2:
                    raise
                time.sleep(5 * (attempt + 1))


def verify_install(assets, base_url, version=None):
    with tempfile.TemporaryDirectory(prefix="cli-install-") as directory:
        env = dict(os.environ, VOLCANO_CLI_RELEASES_URL=base_url, VOLCANO_INSTALL_DIR=directory)
        for name in ("VOLCANO_VERSION", "VOLCANO_GITHUB_RELEASES_URL", "VOLCANO_SKIP_SIGNATURE_VERIFICATION"):
            env.pop(name, None)
        if version:
            env["VOLCANO_VERSION"] = version
        subprocess.run(["sh", str(assets / "install.sh")], env=env, check=True, timeout=600)
        result = subprocess.run([str(Path(directory) / "volcano"), "--version"],
                                capture_output=True, text=True, check=True, timeout=30)
        # Unpinned, the edge may still cache an older pointer; any verified stable install proves resolution.
        expected = re.escape(version) if version else r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
        if not re.search(r"(?<![0-9A-Za-z.])" + expected + r"(?![0-9A-Za-z.])", result.stdout):
            raise ValueError(f"installed CLI did not report {version or 'a stable version'}: {result.stdout}")


def verify_promoted_install(assets, base_url):
    # 75 seconds of retries outlast the pointer's 60-second max-age and CloudFront's default
    # 10-second caching of an earlier 4xx; the download distribution sets no error caching TTL.
    for attempt in range(6):
        try:
            return verify_install(assets, base_url)
        except (OSError, subprocess.SubprocessError, ValueError):
            if attempt == 5:
                raise
            time.sleep(15)


def release_checksums(path):
    checksums = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ((?:install[.]sh|volcano-[a-z0-9-]+(?:[.]exe)?)(?:[.]sigstore[.]json)?)", line)
        if not match or match[2] in checksums:
            raise ValueError("invalid release checksum manifest")
        checksums[match[2]] = match[1]
    signed = tuple(sorted(name for name in checksums if name != "install.sh" and not name.endswith(".sigstore.json"))) + ("install.sh",)
    if len(signed) < 2 or set(checksums) != {*signed, *(name + ".sigstore.json" for name in signed)}:
        raise ValueError("incomplete release checksum manifest")
    return signed, checksums


def fetch_published(bucket, version, assets):
    prefix = f"{PREFIX}/download/{version}/"
    published = listed_keys(bucket, prefix)
    if get(bucket, prefix + "SHA256SUMS", assets / "SHA256SUMS", published) is None:
        raise ValueError(f"missing published asset: {prefix}SHA256SUMS")
    signed, checksums = release_checksums(assets / "SHA256SUMS")
    for name, expected in checksums.items():
        if get(bucket, prefix + name, assets / name, published) is None:
            raise ValueError(f"missing published asset: {prefix + name}")
        if digest(assets / name) != expected:
            raise ValueError(f"release checksum mismatch: {prefix + name}")
    return signed


def prune_aliases(bucket, names):
    prefix = f"{PREFIX}/latest/download/"
    expected = {prefix + name for name in names}
    for key, etag in listed_keys(bucket, prefix).items():
        if key not in expected:
            aws("delete-object", "--bucket", bucket, "--key", key, "--if-match", etag)


def publish_immutable(bucket, version, name, assets, existing, published):
    key = f"{PREFIX}/download/{version}/{name}"
    found = get(bucket, key, existing, published)
    if found is None:
        return put(bucket, key, assets / name, IMMUTABLE, if_none_match="*")["ETag"]
    # Reuse existing bundles on retries: keyless signing produces different bytes.
    # Signed files and checksums may never change for an already-published version.
    if name.endswith(".sigstore.json"):
        (assets / name).write_bytes(existing.read_bytes())
    elif digest(existing) != digest(assets / name):
        raise ValueError(f"refusing to overwrite immutable asset: {key}")
    return found["ETag"]


def reconcile(bucket, path):
    pointer = f"{PREFIX}/latest-version"
    try:
        if get(bucket, pointer, path, listed_keys(bucket, pointer)) is None:
            return
        version = path.read_text().strip()
        version_tuple(version)
        source_prefix = f"{PREFIX}/download/{version}/"
        etags = listed_keys(bucket, source_prefix)
        manifest = path.with_name("reconcile-SHA256SUMS")
        if get(bucket, source_prefix + "SHA256SUMS", manifest, etags) is None:
            raise ValueError(f"missing published asset: {source_prefix}SHA256SUMS")
        _, checksums = release_checksums(manifest)
        names = (*checksums, "SHA256SUMS")
    except Exception as error:
        print(f"error: could not read {pointer} to reconcile latest assets: {error!r}", file=sys.stderr)
        return
    aliases = [(name, f"{PREFIX}/latest/download/{name}") for name in names]
    for name, key in (*aliases, ("install.sh", "builds/install.sh")):
        source = f"{PREFIX}/download/{version}/{name}"
        try:
            copy(bucket, source, etags[source], key)
        except Exception as error:
            print(f"error: could not reconcile {key} to {version}; rerun promotion: {error!r}", file=sys.stderr)
    try:
        prune_aliases(bucket, names)
    except Exception as error:
        print(f"error: could not remove extra latest aliases for {version}: {error!r}", file=sys.stderr)


def promote(bucket, version, etags, current, metadata, names):
    pointer = f"{PREFIX}/latest-version"
    # Verification takes minutes; stop before touching latest objects if another promotion won meanwhile.
    if metadata:
        aws("head-object", "--bucket", bucket, "--key", pointer, "--if-match", metadata["ETag"])
    elif pointer in listed_keys(bucket, pointer):
        raise RuntimeError(f"{pointer} was created during publication")
    try:
        # Copy only the verified versioned objects; each copy fails if its source changed.
        for name in names:
            copy(bucket, f"{PREFIX}/download/{version}/{name}", etags[name], f"{PREFIX}/latest/download/{name}")
        # This bootstrap resolves latest-version; it must work with the old pointer too.
        copy(bucket, f"{PREFIX}/download/{version}/install.sh", etags["install.sh"], "builds/install.sh")
        prune_aliases(bucket, names)
        current.write_text(version + "\n")
        condition = {"if_match": metadata["ETag"]} if metadata else {"if_none_match": "*"}
        put(bucket, pointer, current, MUTABLE, **condition)
    except Exception:
        # Re-read the pointer: a failed write may have landed, or another promotion may have advanced it.
        reconcile(bucket, current)
        raise


def publish(assets, version, bucket, base_url, rollback=False, signed=None):
    candidate = version_tuple(version)
    signed = SIGNED if signed is None else signed
    names = (*signed, *(name + ".sigstore.json" for name in signed), "SHA256SUMS")
    for name in names[:-1]:
        if not (assets / name).is_file() or (assets / name).stat().st_size == 0:
            raise ValueError(f"missing release asset: {name}")
    verify_signatures(assets, version, signed)
    with tempfile.TemporaryDirectory(prefix="cli-publish-") as work:
        work = Path(work)
        pointer = f"{PREFIX}/latest-version"
        current = work / "latest-version"
        metadata = get(bucket, pointer, current, listed_keys(bucket, pointer))
        promote_release = rollback or metadata is None or candidate >= version_tuple(current.read_text().strip())
        published = listed_keys(bucket, f"{PREFIX}/download/{version}/")
        etags = {name: publish_immutable(bucket, version, name, assets, work / name, published)
                 for name in names[:-1]}
        verify_signatures(assets, version, signed)
        checksum_names = sorted(names[:-1])
        (assets / "SHA256SUMS").write_text("".join(f"{digest(assets / name)}  {name}\n" for name in checksum_names))
        etags["SHA256SUMS"] = publish_immutable(bucket, version, "SHA256SUMS", assets, work / "SHA256SUMS", published)
        verify_downloads(base_url, version, assets, names)
        verify_install(assets, base_url, version)
        if not promote_release:
            print(f"Published {version}; retained newer latest version")
            return
        if metadata and not rollback:
            # The new bootstrap must resolve the live pointer before it replaces the old one.
            verify_install(assets, base_url)
        promote(bucket, version, etags, current, metadata, names)
        # Fail before the GitHub release is created if installers cannot resolve the public pointer.
        verify_promoted_install(assets, base_url)
        print(f"Promoted {version} at {base_url}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--assets", type=Path)
    source.add_argument("--rollback", action="store_true",
                        help="re-verify and re-promote an already published version")
    parser.add_argument("--version", required=True)
    parser.add_argument("--bucket", required=True)
    parser.add_argument("--base-url", default="https://download.volcano.dev/builds/releases")
    args = parser.parse_args()
    base_url = args.base_url.rstrip("/")
    if not args.rollback:
        publish(args.assets, args.version, args.bucket, base_url)
        return
    with tempfile.TemporaryDirectory(prefix="cli-rollback-") as directory:
        signed = fetch_published(args.bucket, args.version, Path(directory))
        publish(Path(directory), args.version, args.bucket, base_url, rollback=True, signed=signed)


if __name__ == "__main__":
    main()
