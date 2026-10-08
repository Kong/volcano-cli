import contextlib
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("publish_downloads", Path(__file__).with_name("publish-downloads.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)

BASE_URL = "https://download.example/builds/releases"


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.assets = Path(self.tmp.name)
        self.write_assets("v1.2.3")
        self.objects = {}
        self.cache = {}
        self.types = {}
        self.dispositions = {}
        self.writes = []
        self.fail_key = None
        self.before_write = {}
        self.pointer = publisher.PREFIX + "/latest-version"
        self.patches = [patch.object(publisher, "aws", side_effect=self.aws),
                        patch.object(publisher, "verify_signatures"),
                        patch.object(publisher, "verify_downloads"),
                        patch.object(publisher, "verify_install")]
        self.aws_mock, self.signatures, self.cdn, self.install = [p.start() for p in self.patches]
        for p in self.patches:
            self.addCleanup(p.stop)

    def write_assets(self, version):
        for name in (*publisher.SIGNED, *publisher.BUNDLES):
            (self.assets / name).write_bytes(f"{version}:{name}".encode())

    def etag(self, key):
        return '"' + hashlib.sha256(self.objects[key]).hexdigest()[:32] + '"'

    def aws(self, operation, *args):
        def arg(flag):
            return args[args.index(flag) + 1] if flag in args else None

        if operation == "list-objects-v2":
            return {"Contents": [{"Key": key, "ETag": self.etag(key)}
                                 for key in self.objects if key.startswith(arg("--prefix"))]}
        key = arg("--key")
        if operation == "get-object":
            if key not in self.objects:
                # S3 without ListBucket authorization for the GetObject request.
                raise RuntimeError("An error occurred (AccessDenied)")
            Path(args[-1]).write_bytes(self.objects[key])
            return {"ETag": self.etag(key)}
        if operation == "head-object":
            if key not in self.objects or arg("--if-match") not in (None, self.etag(key)):
                raise RuntimeError("An error occurred (PreconditionFailed)")
            return {"ETag": self.etag(key)}
        self.before_write.pop(key, lambda: None)()
        if key == self.pointer:
            self.assertTrue({"--if-match", "--if-none-match"} & set(args), "pointer writes must be conditional")
        if key == self.fail_key:
            self.fail_key = None
            raise RuntimeError("upload failed")
        if arg("--if-none-match") == "*" and key in self.objects:
            raise RuntimeError("An error occurred (PreconditionFailed)")
        if "--if-match" in args and (key not in self.objects or arg("--if-match") != self.etag(key)):
            raise RuntimeError("An error occurred (PreconditionFailed)")
        if operation == "delete-object":
            self.assertEqual(arg("--if-match"), self.etag(key))
            for store in (self.objects, self.cache, self.types, self.dispositions):
                store.pop(key, None)
            self.writes.append(key)
            return {}
        if operation == "copy-object":
            source = arg("--copy-source").split("/", 1)[1]
            if source not in self.objects or arg("--copy-source-if-match") != self.etag(source):
                raise RuntimeError("An error occurred (PreconditionFailed)")
            body = self.objects[source]
        else:
            body = Path(arg("--body")).read_bytes()
        self.objects[key] = body
        self.cache[key] = arg("--cache-control")
        self.types[key] = arg("--content-type")
        self.dispositions[key] = arg("--content-disposition")
        self.writes.append(key)
        return {"ETag": self.etag(key)}

    def publish(self, version="v1.2.3"):
        publisher.publish(self.assets, version, "bucket", BASE_URL)

    def versioned(self, version, name):
        return f"{publisher.PREFIX}/download/{version}/{name}"

    def alias(self, name):
        return f"{publisher.PREFIX}/latest/download/{name}"

    def promoted_keys(self):
        return [*(self.alias(name) for name in publisher.ASSETS), "builds/install.sh", self.pointer]

    def latest(self):
        return {key: self.objects.get(key) for key in self.promoted_keys()}

    def released(self):
        return {key: value for key, value in self.objects.items() if key.startswith(publisher.PREFIX + "/download/")}

    def seed(self, version):
        self.write_assets(version)
        self.publish(version)
        self.write_assets("v1.2.3")
        self.writes.clear()
        return self.latest()

    def assert_latest_is(self, version):
        self.assertEqual(self.objects[self.pointer], f"{version}\n".encode())
        for name in publisher.ASSETS:
            self.assertEqual(self.objects[self.alias(name)], self.objects[self.versioned(version, name)], name)
        self.assertEqual(self.objects["builds/install.sh"], self.objects[self.versioned(version, "install.sh")])

    def test_promotes_only_after_all_assets_verified(self):
        def verify(*_):
            for key in self.promoted_keys():
                self.assertNotIn(key, self.objects)
            for name in publisher.ASSETS:
                self.assertIn(self.versioned("v1.2.3", name), self.objects)
        self.cdn.side_effect = verify
        self.publish()
        self.assertEqual(self.writes[-len(self.promoted_keys()):], self.promoted_keys())
        self.assert_latest_is("v1.2.3")
        sums = self.objects[self.versioned("v1.2.3", "SHA256SUMS")].decode().splitlines()
        self.assertEqual([line.split("  ")[1] for line in sums], sorted((*publisher.SIGNED, *publisher.BUNDLES)))
        self.assertEqual(self.signatures.call_count, 2)

    def test_publishes_windows_arm64_binary_and_signature(self):
        self.publish()
        name = "volcano-windows-arm64.exe"
        for asset in (name, name + ".sigstore.json"):
            versioned = self.versioned("v1.2.3", asset)
            self.assertIn(versioned, self.objects)
            self.assertEqual(self.objects[self.alias(asset)], self.objects[versioned])
            sums = self.objects[self.versioned("v1.2.3", "SHA256SUMS")].decode()
            expected = hashlib.sha256(self.objects[versioned]).hexdigest()
            self.assertIn(f"{expected}  {asset}\n", sums)
        self.assertEqual(self.dispositions[self.alias(name)], f"attachment; filename={name}")

    def test_objects_are_served_with_readable_content_types(self):
        self.publish()
        text = "text/plain; charset=utf-8"
        for key, expected in {
            self.versioned("v1.2.3", publisher.BINARIES[0]): "application/octet-stream",
            self.versioned("v1.2.3", "install.sh"): text,
            self.versioned("v1.2.3", "install.sh.sigstore.json"): "application/json",
            self.versioned("v1.2.3", "SHA256SUMS"): text,
            self.alias(publisher.BINARIES[-1]): "application/octet-stream",
            self.alias("SHA256SUMS"): text,
            "builds/install.sh": text,
            self.pointer: text,
        }.items():
            self.assertEqual(self.types[key], expected, key)

    def test_binaries_download_under_their_asset_names(self):
        self.seed("v1.2.2")
        self.publish()
        self.rollback("v1.2.2")
        for version in ("v1.2.2", "v1.2.3"):
            for name in publisher.ASSETS:
                expected = f"attachment; filename={name}" if name in publisher.BINARIES else None
                self.assertEqual(self.dispositions[self.versioned(version, name)], expected, name)
        for name in publisher.ASSETS:
            expected = f"attachment; filename={name}" if name in publisher.BINARIES else None
            self.assertEqual(self.dispositions[self.alias(name)], expected, name)
            if name in publisher.BINARIES:
                self.assertEqual(self.types[self.alias(name)], "application/octet-stream", name)
                self.assertEqual(self.cache[self.alias(name)], publisher.MUTABLE, name)
        for key in ("builds/install.sh", self.pointer):
            self.assertIsNone(self.dispositions[key], key)

    def test_bootstrap_must_resolve_live_pointer_before_promotion(self):
        self.seed("v1.2.2")
        self.install.reset_mock()
        self.publish()
        self.assertEqual([call.args[1:] for call in self.install.call_args_list],
                         [(BASE_URL, "v1.2.3"), (BASE_URL,), (BASE_URL,)])

        previous = self.latest()

        def bootstrap_fails(_assets, _base_url, version=None):
            if version is None:
                raise ValueError("bootstrap could not resolve latest-version")
        self.install.side_effect = bootstrap_fails
        self.write_assets("v1.2.4")
        with self.assertRaisesRegex(ValueError, "could not resolve"):
            self.publish("v1.2.4")
        self.assertEqual(self.latest(), previous)

    def test_first_publication_installs_through_the_promoted_pointer(self):
        pointers = []

        def install(_assets, _base_url, version=None):
            if version is None:
                pointers.append(self.objects.get(self.pointer))
        self.install.side_effect = install
        self.publish()
        self.assertEqual(pointers, [b"v1.2.3\n"])

    def test_promoted_install_retries_then_fails_the_release(self):
        attempts = []

        def install(_assets, _base_url, version=None):
            if version is None:
                attempts.append(version)
                raise ValueError("could not resolve latest")
        self.install.side_effect = install
        with patch.object(publisher.time, "sleep") as sleep, self.assertRaisesRegex(ValueError, "could not resolve"):
            self.publish()
        self.assertEqual(len(attempts), 6)
        self.assertEqual(sleep.call_count, 5)
        self.assert_latest_is("v1.2.3")

    def test_promoted_install_succeeds_once_caches_expire(self):
        failures = iter([True, True, False])

        def install(_assets, _base_url, version=None):
            if version is None and next(failures):
                raise publisher.subprocess.CalledProcessError(1, "sh")
        self.install.side_effect = install
        with patch.object(publisher.time, "sleep") as sleep:
            self.publish()
        self.assertEqual(sleep.call_count, 2)

    def test_latest_objects_use_pointer_cache_lifetime(self):
        self.publish()
        for name in publisher.ASSETS:
            self.assertEqual(self.cache[self.versioned("v1.2.3", name)], publisher.IMMUTABLE)
        for key in self.promoted_keys():
            self.assertEqual(self.cache[key], publisher.MUTABLE, key)

    def test_missing_asset_prevents_any_upload(self):
        (self.assets / publisher.BINARIES[-1]).unlink()
        with self.assertRaisesRegex(ValueError, "missing release asset"):
            self.publish()
        self.assertFalse(self.writes)

    def test_partial_upload_keeps_previous_release(self):
        previous = self.seed("v1.2.2")
        self.fail_key = self.versioned("v1.2.3", publisher.BINARIES[1])
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_failed_alias_copy_reconciles_to_pointer(self):
        previous = self.seed("v1.2.2")
        self.fail_key = self.alias(publisher.ASSETS[3])
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_failed_bootstrap_copy_reconciles_to_pointer(self):
        previous = self.seed("v1.2.2")
        self.fail_key = "builds/install.sh"
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_failed_pointer_write_reconciles_to_unchanged_pointer(self):
        previous = self.seed("v1.2.2")
        self.fail_key = self.pointer
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_pointer_write_that_landed_keeps_new_latest(self):
        self.seed("v1.2.2")
        self.before_write[self.pointer] = lambda: self.objects.update({self.pointer: b"v1.2.3\n"})
        with self.assertRaisesRegex(RuntimeError, "PreconditionFailed"):
            self.publish()
        self.assert_latest_is("v1.2.3")

    def test_newer_concurrent_promotion_wins_reconcile(self):
        self.seed("v1.2.2")
        for name in publisher.ASSETS[:-1]:
            self.objects[self.versioned("v1.2.4", name)] = f"v1.2.4:{name}".encode()
        self.objects[self.versioned("v1.2.4", "SHA256SUMS")] = "".join(
            f'{hashlib.sha256(self.objects[self.versioned("v1.2.4", name)]).hexdigest()}  {name}\n'
            for name in sorted(publisher.ASSETS[:-1])
        ).encode()
        self.before_write[self.pointer] = lambda: self.objects.update({self.pointer: b"v1.2.4\n"})
        with self.assertRaisesRegex(RuntimeError, "PreconditionFailed"):
            self.publish()
        self.assert_latest_is("v1.2.4")

    def test_failed_reconcile_is_reported_and_still_fails(self):
        previous = self.seed("v1.2.2")
        del self.objects[self.versioned("v1.2.2", publisher.ASSETS[0])]
        self.fail_key = self.alias(publisher.ASSETS[1])
        stderr = io.StringIO()
        with contextlib.redirect_stderr(stderr), self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertIn("could not reconcile " + self.alias(publisher.ASSETS[0]), stderr.getvalue())
        self.assertEqual(self.objects[self.alias(publisher.ASSETS[1])], previous[self.alias(publisher.ASSETS[1])])
        self.assertEqual(self.objects[self.pointer], previous[self.pointer])

    def test_first_promotion_has_nothing_to_reconcile(self):
        self.fail_key = self.alias(publisher.ASSETS[1])
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual([key for key in self.writes if key.startswith(self.alias(""))], [self.alias(publisher.ASSETS[0])])
        self.assertNotIn(self.pointer, self.objects)

    def test_cdn_failure_does_not_promote(self):
        previous = self.seed("v1.2.2")
        self.cdn.side_effect = ValueError("checksum mismatch")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_install_failure_does_not_promote(self):
        previous = self.seed("v1.2.2")
        self.install.side_effect = ValueError("installation failed")
        with self.assertRaisesRegex(ValueError, "installation failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_latest_copies_only_verified_objects(self):
        previous = self.seed("v1.2.2")

        def replace_after_verification(*_args, **_kwargs):
            self.objects[self.versioned("v1.2.3", publisher.ASSETS[0])] = b"replaced"
        self.install.side_effect = replace_after_verification
        with self.assertRaisesRegex(RuntimeError, "PreconditionFailed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_rerun_reuses_signatures_and_never_rewrites_versioned_assets(self):
        self.publish()
        before = dict(self.objects)
        self.writes.clear()
        for name in publisher.SIGNED:
            (self.assets / (name + ".sigstore.json")).write_bytes(b"new keyless bundle")
        self.publish()
        self.assertEqual(before, self.objects)
        self.assertEqual(self.writes, self.promoted_keys())

    def test_changed_binary_cannot_overwrite_release(self):
        self.publish()
        (self.assets / publisher.BINARIES[0]).write_bytes(b"changed binary")
        with self.assertRaisesRegex(ValueError, "immutable asset"):
            self.publish()

    def test_old_release_does_not_downgrade_latest(self):
        newer = self.seed("v2.0.0")
        self.publish()
        self.assertEqual(self.latest(), newer)
        self.assertIn(self.versioned("v1.2.3", publisher.BINARIES[0]), self.objects)

    def test_concurrent_promotion_fails_closed(self):
        previous = self.seed("v1.2.2")

        def promoted_elsewhere(*_args, **_kwargs):
            self.objects[self.pointer] = b"v1.2.4\n"
        self.install.side_effect = promoted_elsewhere
        with self.assertRaisesRegex(RuntimeError, "PreconditionFailed"):
            self.publish()
        self.assertEqual(self.latest(), {**previous, self.pointer: b"v1.2.4\n"})

    def test_first_promotion_stops_if_pointer_appears(self):
        def promoted_elsewhere(*_args, **_kwargs):
            self.objects[self.pointer] = b"v1.2.4\n"
        self.install.side_effect = promoted_elsewhere
        with self.assertRaisesRegex(RuntimeError, "created during publication"):
            self.publish()
        self.assertEqual(self.latest(), {**dict.fromkeys(self.promoted_keys()), self.pointer: b"v1.2.4\n"})

    def test_invalid_pointer_fails_closed(self):
        self.objects[self.pointer] = b"garbage"
        with self.assertRaisesRegex(ValueError, "invalid release version"):
            self.publish()
        self.assertFalse(self.writes)

    def rollback(self, version):
        argv = ["publish-downloads.py", "--rollback", "--version", version, "--bucket", "bucket", "--base-url", BASE_URL]
        with patch.object(sys, "argv", argv):
            publisher.main()

    def test_rollback_restores_previous_latest(self):
        self.seed("v1.2.2")
        self.publish()
        released = self.released()
        self.writes.clear()
        self.rollback("v1.2.2")
        self.assert_latest_is("v1.2.2")
        self.assertEqual(self.writes, self.promoted_keys())
        self.assertEqual(self.released(), released)

    def seed_without_windows_arm64(self):
        signed = tuple(name for name in publisher.SIGNED if name != "volcano-windows-arm64.exe")
        bundles = tuple(name + ".sigstore.json" for name in signed)
        with patch.multiple(publisher, SIGNED=signed, BUNDLES=bundles, ASSETS=(*signed, *bundles, "SHA256SUMS")):
            self.seed("v1.2.2")

    def test_rollback_removes_aliases_absent_from_older_release(self):
        self.seed_without_windows_arm64()
        self.publish()
        for name in ("volcano-future-arm64.exe", "volcano-future-arm64.exe.sigstore.json"):
            self.objects[self.alias(name)] = b"future asset"
        untouched = publisher.PREFIX + "/latest/keep.txt"
        self.objects[untouched] = b"outside download aliases"
        released = self.released()
        self.rollback("v1.2.2")
        self.assertEqual(self.objects[self.pointer], b"v1.2.2\n")
        for name in ("volcano-windows-arm64.exe", "volcano-windows-arm64.exe.sigstore.json",
                     "volcano-future-arm64.exe", "volcano-future-arm64.exe.sigstore.json"):
            self.assertNotIn(self.alias(name), self.objects)
        self.assertEqual(self.objects[self.alias("SHA256SUMS")],
                         self.objects[self.versioned("v1.2.2", "SHA256SUMS")])
        self.assertEqual(self.released(), released)
        self.assertEqual(self.objects[untouched], b"outside download aliases")

    def test_failed_rollback_restores_deleted_aliases(self):
        self.seed_without_windows_arm64()
        self.publish()
        latest = self.latest()
        self.fail_key = self.pointer
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.rollback("v1.2.2")
        self.assertEqual(self.latest(), latest)

    def test_alias_deletion_failure_keeps_previous_pointer(self):
        self.seed_without_windows_arm64()
        self.publish()
        latest = self.latest()
        self.fail_key = self.alias("volcano-windows-arm64.exe")
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.rollback("v1.2.2")
        self.assertEqual(self.latest(), latest)

    def test_failed_first_arm64_promotion_removes_new_aliases(self):
        self.seed_without_windows_arm64()
        previous = self.latest()
        self.fail_key = self.pointer
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.latest(), previous)

    def test_rollback_rejects_unsafe_or_corrupt_manifest_before_writes(self):
        for manifest in (b"garbage\n", b"0" * 64 + b"  ../escape\n",
                         b"0" * 64 + b"  install.sh\n", (b"0" * 64 + b"  install.sh\n") * 2):
            with self.subTest(manifest=manifest):
                self.objects[self.versioned("v1.2.2", "SHA256SUMS")] = manifest
                with self.assertRaisesRegex(ValueError, "release checksum manifest"):
                    self.rollback("v1.2.2")
                self.assertFalse(self.writes)

    def test_rollback_rejects_changed_release_bytes_before_writes(self):
        self.seed("v1.2.2")
        self.publish()
        latest = self.latest()
        self.objects[self.versioned("v1.2.2", "volcano-linux-amd64")] = b"changed bytes"
        self.writes.clear()
        with self.assertRaisesRegex(ValueError, "release checksum mismatch"):
            self.rollback("v1.2.2")
        self.assertFalse(self.writes)
        self.assertEqual(self.latest(), latest)

    def test_rollback_requires_complete_release(self):
        self.seed("v1.2.2")
        self.publish()
        latest = self.latest()
        del self.objects[self.versioned("v1.2.2", "SHA256SUMS")]
        self.writes.clear()
        with self.assertRaisesRegex(ValueError, "missing published asset"):
            self.rollback("v1.2.2")
        self.assertFalse(self.writes)
        self.assertEqual(self.latest(), latest)

    def test_base_url_must_be_https(self):
        for base_url in ("http://download.example/builds/releases", "file:///tmp/releases", "https:///builds",
                         "https://:443/builds/releases", "https://user@/builds/releases"):
            argv = ["publish-downloads.py", "--assets", str(self.assets), "--version", "v1.2.3",
                    "--bucket", "bucket", "--base-url", base_url]
            with self.subTest(base_url=base_url), patch.object(sys, "argv", argv), \
                    contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as exited:
                publisher.main()
            self.assertEqual(exited.exception.code, 2)
        self.assertFalse(self.writes)

    def test_permissions_are_not_missing_objects(self):
        with patch.object(publisher.subprocess, "run") as run:
            run.return_value.returncode = 1
            run.return_value.stderr = "An error occurred (AccessDenied)"
            with self.assertRaisesRegex(RuntimeError, "AccessDenied"):
                # Call the actual adapter, not this suite's fake store.
                spec2 = importlib.util.spec_from_file_location("real_publisher", Path(__file__).with_name("publish-downloads.py"))
                real = importlib.util.module_from_spec(spec2)
                spec2.loader.exec_module(real)
                real.aws("get-object", "--bucket", "b", "--key", "k", "/tmp/unused")


class DownloadVerificationTests(unittest.TestCase):
    def test_install_check_pins_only_when_asked(self):
        installs = []

        def run(args, env=None, **_kwargs):
            if env is not None:
                installs.append(env)
            return SimpleNamespace(stdout="volcano v1.2.2 (abc1234, 2026-10-01T00:00:00Z)\n")
        inherited = {"VOLCANO_VERSION": "v9.9.9", "VOLCANO_SKIP_SIGNATURE_VERIFICATION": "1",
                     "VOLCANO_GITHUB_RELEASES_URL": "https://legacy.example/releases"}
        with patch.object(publisher.subprocess, "run", side_effect=run), patch.dict(os.environ, inherited):
            publisher.verify_install(Path("assets"), BASE_URL, "v1.2.2")
            publisher.verify_install(Path("assets"), BASE_URL)
            with self.assertRaisesRegex(ValueError, "did not report v1.2.3"):
                publisher.verify_install(Path("assets"), BASE_URL, "v1.2.3")
        pinned, unpinned, _ = installs
        self.assertEqual(pinned["VOLCANO_VERSION"], "v1.2.2")
        self.assertNotIn("VOLCANO_VERSION", unpinned)
        for env in installs:
            self.assertEqual(env["VOLCANO_CLI_RELEASES_URL"], BASE_URL)
            self.assertNotIn("VOLCANO_SKIP_SIGNATURE_VERIFICATION", env)
            self.assertNotIn("VOLCANO_GITHUB_RELEASES_URL", env)

    def test_retries_truncated_cdn_reads(self):
        import io

        class Truncated(io.BytesIO):
            def read(self, *_args):
                raise publisher.http.client.IncompleteRead(b"exp")
        responses = iter([Truncated(), io.BytesIO(b"expected")])
        with tempfile.TemporaryDirectory() as directory:
            assets = Path(directory)
            (assets / publisher.ASSETS[0]).write_bytes(b"expected")
            with patch.object(publisher.urllib.request, "urlopen", side_effect=lambda *_args, **_kwargs: next(responses)) as fetch, \
                    patch.object(publisher, "ASSETS", publisher.ASSETS[:1]), patch.object(publisher.time, "sleep"):
                publisher.verify_downloads("https://download.example", "v1.2.3", assets)
            self.assertEqual(fetch.call_count, 2)

    def test_rejects_corrupt_cdn_bytes(self):
        import io
        with tempfile.TemporaryDirectory() as directory:
            assets = Path(directory)
            for name in publisher.ASSETS:
                (assets / name).write_bytes(b"expected")
            with patch.object(publisher.urllib.request, "urlopen", side_effect=lambda *_args, **_kwargs: io.BytesIO(b"corrupt")), patch.object(publisher.time, "sleep"):
                with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                    publisher.verify_downloads("https://download.example", "v1.2.3", assets)

    def test_verifies_every_asset(self):
        import io
        with tempfile.TemporaryDirectory() as directory:
            assets = Path(directory)
            for name in publisher.ASSETS:
                (assets / name).write_bytes(b"expected")
            with patch.object(publisher.urllib.request, "urlopen", side_effect=lambda *_args, **_kwargs: io.BytesIO(b"expected")) as fetch:
                publisher.verify_downloads("https://download.example", "v1.2.3", assets)
            self.assertEqual(fetch.call_count, len(publisher.ASSETS))


if __name__ == "__main__":
    unittest.main()
