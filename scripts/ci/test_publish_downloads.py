import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("publish_downloads", Path(__file__).with_name("publish-downloads.py"))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class PublishTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.assets = Path(self.tmp.name)
        for name in publisher.ASSETS:
            (self.assets / name).write_bytes(name.encode())
        self.objects = {}
        self.writes = []
        self.fail_key = None
        self.pointer = publisher.PREFIX + "/latest-version"
        self.patches = [patch.object(publisher, "aws", side_effect=self.aws),
                        patch.object(publisher, "verify_signatures"),
                        patch.object(publisher, "verify_downloads"),
                        patch.object(publisher, "verify_install")]
        self.aws_mock, self.signatures, self.cdn, self.install = [p.start() for p in self.patches]
        for p in self.patches:
            self.addCleanup(p.stop)

    def aws(self, operation, *args):
        key = args[args.index("--key") + 1]
        if operation == "get-object":
            if key not in self.objects:
                return None
            Path(args[-1]).write_bytes(self.objects[key])
            return {"ETag": '"old-etag"'}
        if key == self.fail_key:
            raise RuntimeError("upload failed")
        if "--if-none-match" in args and key in self.objects:
            raise RuntimeError("precondition failed")
        if key == self.pointer and key in self.objects:
            self.assertIn("--if-match", args)
        path = Path(args[args.index("--body") + 1])
        self.objects[key] = path.read_bytes()
        self.writes.append(key)
        return {}

    def publish(self, version="v1.2.3"):
        publisher.publish(self.assets, version, "bucket", "https://download.example/builds/releases")

    def test_promotes_only_after_all_assets_verified(self):
        def verify(*_):
            self.assertNotIn(self.pointer, self.objects)
            for name in publisher.ASSETS:
                self.assertIn(f"{publisher.PREFIX}/download/v1.2.3/{name}", self.objects)
        self.cdn.side_effect = verify
        self.publish()
        self.assertEqual(self.writes[-1], self.pointer)
        self.assertEqual(self.objects[self.pointer], b"v1.2.3\n")
        self.assertEqual(self.signatures.call_count, 2)

    def test_missing_asset_prevents_any_upload(self):
        (self.assets / publisher.BINARIES[-1]).unlink()
        with self.assertRaisesRegex(ValueError, "missing release asset"):
            self.publish()
        self.assertFalse(self.writes)

    def test_partial_upload_keeps_previous_release(self):
        self.objects[self.pointer] = b"v1.2.2\n"
        self.fail_key = f"{publisher.PREFIX}/download/v1.2.3/{publisher.BINARIES[1]}"
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.objects[self.pointer], b"v1.2.2\n")
        self.assertNotIn("builds/install.sh", self.objects)

    def test_cdn_failure_does_not_promote(self):
        self.cdn.side_effect = ValueError("checksum mismatch")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            self.publish()
        self.assertNotIn(self.pointer, self.objects)
        self.assertNotIn("builds/install.sh", self.objects)

    def test_install_failure_does_not_promote(self):
        self.install.side_effect = ValueError("installation failed")
        with self.assertRaisesRegex(ValueError, "installation failed"):
            self.publish()
        self.assertNotIn(self.pointer, self.objects)
        self.assertNotIn("builds/install.sh", self.objects)

    def test_rerun_reuses_signatures_and_never_rewrites_versioned_assets(self):
        self.publish()
        before = dict(self.objects)
        self.writes.clear()
        for name in publisher.SIGNED:
            (self.assets / (name + ".sigstore.json")).write_bytes(b"new keyless bundle")
        self.publish()
        self.assertEqual(before, self.objects)
        self.assertEqual(self.writes, ["builds/install.sh", self.pointer])

    def test_changed_binary_cannot_overwrite_release(self):
        self.publish()
        (self.assets / publisher.BINARIES[0]).write_bytes(b"changed binary")
        with self.assertRaisesRegex(ValueError, "immutable asset"):
            self.publish()

    def test_old_release_does_not_downgrade_bootstrap_or_pointer(self):
        self.objects[self.pointer] = b"v2.0.0\n"
        self.objects["builds/install.sh"] = b"newer bootstrap"
        self.publish()
        self.assertEqual(self.objects[self.pointer], b"v2.0.0\n")
        self.assertEqual(self.objects["builds/install.sh"], b"newer bootstrap")

    def test_concurrent_promotion_fails_closed(self):
        self.objects[self.pointer] = b"v1.2.2\n"
        self.fail_key = self.pointer
        with self.assertRaisesRegex(RuntimeError, "upload failed"):
            self.publish()
        self.assertEqual(self.objects[self.pointer], b"v1.2.2\n")

    def test_invalid_pointer_fails_closed(self):
        self.objects[self.pointer] = b"garbage"
        with self.assertRaisesRegex(ValueError, "invalid release version"):
            self.publish()
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
