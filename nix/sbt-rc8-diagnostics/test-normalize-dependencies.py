import hashlib
import pathlib
import subprocess
import sys
import tempfile
import unittest

SCRIPT = sys.argv.pop()


class NormalizationTest(unittest.TestCase):
    def test_preserves_artifact_bytes_and_removes_only_markers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            artifact = root / "library.jar"
            artifact.write_bytes(b"fixed immutable artifact bytes")
            before = hashlib.sha256(artifact.read_bytes()).hexdigest()
            for name in ["sbt.boot.lock", "update.log", "artifact.checked", ".checked"]:
                (root / name).write_text("time-dependent marker")
            (root / "library.jar.sha1").write_text("retained checksum")
            (root / "ivydata.properties").write_text("retained offline resolver metadata")
            (root / "global/cache").mkdir(parents=True)
            (root / "global/cache/path-dependent.bin").write_text("temporary resolution state")
            subprocess.run([sys.executable, SCRIPT, directory], check=True)
            self.assertEqual(hashlib.sha256(artifact.read_bytes()).hexdigest(), before)
            self.assertEqual(sorted(p.name for p in root.iterdir()), ["global", "ivydata.properties", "library.jar", "library.jar.sha1"])
            self.assertFalse((root / "global/cache").exists())
            subprocess.run([sys.executable, SCRIPT, directory], check=True)
            self.assertEqual(hashlib.sha256(artifact.read_bytes()).hexdigest(), before)

    def test_refuses_cache_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "escape.jar").symlink_to("/outside")
            result = subprocess.run([sys.executable, SCRIPT, directory], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b"resolver cache symlink", result.stderr)


unittest.main()
