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

    def test_global_ancestor_symlink_cannot_delete_external_cache(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            root, outside = base / "root", base / "outside"
            root.mkdir()
            (outside / "cache").mkdir(parents=True)
            sentinel = outside / "cache/owned-by-another-context"
            sentinel.write_bytes(b"must survive")
            (root / "global").symlink_to(outside, target_is_directory=True)
            result = subprocess.run([sys.executable, SCRIPT, str(root)], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(sentinel.read_bytes(), b"must survive")

    def test_root_and_root_ancestor_symlinks_are_refused_before_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            (base / "real/cache").mkdir(parents=True)
            sentinel = base / "real/cache/update.log"
            sentinel.write_text("preserved by refusal")
            (base / "alias").symlink_to(base / "real", target_is_directory=True)
            for root in [base / "alias", base / "alias/cache"]:
                result = subprocess.run([sys.executable, SCRIPT, str(root)], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(sentinel.read_text(), "preserved by refusal")

    def test_late_symlink_does_not_allow_partial_marker_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            marker = root / "a.lock"
            marker.write_bytes(b"must survive the refused normalization")
            (root / "z.jar").symlink_to("/outside")
            result = subprocess.run([sys.executable, SCRIPT, directory], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(marker.read_bytes(), b"must survive the refused normalization")


unittest.main()
