"""The Registry checksum inventory contains packages and manifest, not extra assets."""
import hashlib
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class ReleaseChecksumsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.directory = Path(self.tmp.name)
        self.assets = {
            "terraform-provider-allot_0.1.1_linux_amd64.zip": b"provider archive fixture",
            "terraform-provider-allot_0.1.1_manifest.json": b'{"version":1}',
        }
        self.manifest = self.directory / "terraform-provider-allot_0.1.1_SHA256SUMS"

    def write(self, extra=False):
        assets = dict(self.assets)
        if extra:
            assets["signing-key.asc"] = b"public key fixture"
        for name, data in assets.items():
            (self.directory / name).write_bytes(data)
        self.manifest.write_text("".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in assets.items()))

    def run_cli(self, *args):
        return subprocess.run([sys.executable, str(Path(__file__).with_name("release_checksums.py")), str(self.directory), *args], capture_output=True, text=True)

    def test_accepts_packages_and_manifest(self):
        self.write()
        self.assertEqual(self.run_cli().returncode, 0)

    def test_rejects_public_key_in_package_checksums(self):
        self.write(extra=True)
        result = self.run_cli()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected=['signing-key.asc']", result.stderr)

    def test_repair_preserves_packages_and_their_checksums(self):
        self.write(extra=True)
        expected = "".join(f"{hashlib.sha256(data).hexdigest()}  {name}\n" for name, data in self.assets.items())
        self.assertEqual(self.run_cli("--remove-public-key").returncode, 0)
        self.assertEqual(self.manifest.read_text(), expected)
        for name, data in self.assets.items():
            self.assertEqual((self.directory / name).read_bytes(), data)
        self.assertTrue((self.directory / "signing-key.asc").is_file())
        self.assertEqual(self.run_cli().returncode, 0)

    def test_repair_rejects_corrupt_packages_without_changing_metadata(self):
        self.write(extra=True)
        before = self.manifest.read_bytes()
        (self.directory / "terraform-provider-allot_0.1.1_linux_amd64.zip").write_bytes(b"corrupt")
        self.assertNotEqual(self.run_cli("--remove-public-key").returncode, 0)
        self.assertEqual(self.manifest.read_bytes(), before)


if __name__ == "__main__":
    unittest.main()
