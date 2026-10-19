#!/usr/bin/env python3
"""Finite ownership and declaration tests; no Nix, Go, network or user cache."""
import importlib.util
import os
import signal
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("binding", Path(__file__).with_name("devctl-ci-dependencies.py"))
binding = importlib.util.module_from_spec(spec)
spec.loader.exec_module(binding)


class BindingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.module = self.root / "cli/devctl"
        self.module.mkdir(parents=True)
        self.supplier = self.root / "supplier"
        (self.supplier / "vendor").mkdir(parents=True)
        (self.supplier / "vendor/modules.txt").write_text("fixture\n")
        for name in ("go.mod", "go.sum"):
            (self.module / name).write_text(name)
            (self.supplier / name).write_text(name)
        self.vendor = self.module / "vendor"

    def run_child(self, source):
        return binding.run(self.supplier, [sys.executable, "-c", source], self.root)

    def test_inherits_flags_and_cleans_failed_child(self):
        with patch.dict(os.environ, {"GOFLAGS": "-p=1"}):
            code = self.run_child("import os,pathlib,sys; assert os.environ['GOFLAGS']=='-p=1 -mod=vendor'; assert pathlib.Path('cli/devctl/vendor/modules.txt').read_text()=='fixture\\n'; sys.exit(7)")
        self.assertEqual(code, 7)
        self.assertFalse(os.path.lexists(self.vendor))

    def test_declaration_mismatch_refuses_before_child(self):
        for name in ("go.mod", "go.sum"):
            with self.subTest(name=name):
                (self.module / name).write_text("changed")
                with self.assertRaises(ValueError):
                    self.run_child("raise AssertionError('must not start')")
                self.assertFalse(os.path.lexists(self.vendor))
                (self.module / name).write_text(name)

    def test_preserves_all_existing_path_types(self):
        for kind in ("directory", "file", "dangling-link"):
            with self.subTest(kind=kind):
                if kind == "directory":
                    self.vendor.mkdir()
                elif kind == "file":
                    self.vendor.write_text("protected")
                else:
                    self.vendor.symlink_to("absent")
                before = self.vendor.lstat()
                with self.assertRaises(FileExistsError):
                    self.run_child("raise AssertionError('must not start')")
                self.assertEqual(self.vendor.lstat().st_ino, before.st_ino)
                if kind == "directory":
                    self.vendor.rmdir()
                else:
                    self.vendor.unlink()

    def test_preserves_replacement_after_child(self):
        self.assertEqual(self.run_child("from pathlib import Path; p=Path('cli/devctl/vendor'); p.unlink(); p.mkdir(); (p/'owned-by-other').write_text('keep')"), 0)
        self.assertEqual((self.vendor / "owned-by-other").read_text(), "keep")

    def test_refuses_alternate_module_file(self):
        with patch.dict(os.environ, {"GOFLAGS": "-modfile=other.mod"}):
            with self.assertRaises(ValueError):
                self.run_child("raise AssertionError('must not start')")
        self.assertFalse(os.path.lexists(self.vendor))

    def test_missing_command_cleans_binding(self):
        with self.assertRaises(FileNotFoundError):
            binding.run(self.supplier, [str(self.root / "no-command")], self.root)
        self.assertFalse(os.path.lexists(self.vendor))

    def test_signal_forwarding_retains_interrupted_binding(self):
        harness = """import importlib.util, pathlib, sys
s=importlib.util.spec_from_file_location('binding',sys.argv[1]); m=importlib.util.module_from_spec(s); s.loader.exec_module(m)
sys.exit(m.run(pathlib.Path(sys.argv[2]), [sys.executable,'-c', 'import os,signal,time; os.kill(os.getppid(),signal.SIGTERM); time.sleep(30)'], pathlib.Path(sys.argv[3])))
"""
        result = subprocess.run([sys.executable, "-B", "-c", harness, str(Path(binding.__file__)), str(self.supplier), str(self.root)], timeout=5)
        self.assertEqual(result.returncode, 143)
        self.assertTrue(self.vendor.is_symlink())
        with self.assertRaises(FileExistsError):
            self.run_child("raise AssertionError('must not start')")


    def test_child_catching_cancellation_cannot_report_success(self):
        code = self.run_child("import os,signal,sys,time; signal.signal(signal.SIGTERM,lambda *_: sys.exit(0)); os.kill(os.getppid(),signal.SIGTERM); time.sleep(30)")
        self.assertEqual(code, 143)
        self.assertTrue(self.vendor.is_symlink())

    def test_cancellation_reaches_grandchild_group(self):
        grandchild = "import pathlib,signal,sys,time; signal.signal(signal.SIGTERM,lambda *_: (pathlib.Path('grandchild-stopped').write_text('term'),sys.exit(0))); pathlib.Path('grandchild-ready').touch(); time.sleep(30)"
        child = ("import os,pathlib,signal,subprocess,sys,time; "
                 f"subprocess.Popen([sys.executable,'-c',{grandchild!r}]); "
                 "exec(\"while not pathlib.Path('grandchild-ready').exists(): time.sleep(.01)\"); "
                 "os.kill(os.getppid(),signal.SIGTERM); time.sleep(30)")
        code = self.run_child(child)
        self.assertEqual(code, 143)
        deadline = time.monotonic() + 3
        while not (self.root / "grandchild-stopped").exists() and time.monotonic() < deadline:
            time.sleep(.01)
        self.assertEqual((self.root / "grandchild-stopped").read_text(), "term")
        self.assertTrue(self.vendor.is_symlink())

    def test_signal_during_creation_is_recorded_and_binding_retained(self):
        real_popen = subprocess.Popen

        def interrupted_spawn(*args, **kwargs):
            os.kill(os.getpid(), signal.SIGTERM)
            return real_popen(*args, **kwargs)

        with patch.object(binding.subprocess, "Popen", interrupted_spawn):
            code = self.run_child("import time; time.sleep(30)")
        self.assertEqual(code, 143)
        self.assertTrue(self.vendor.is_symlink())


if __name__ == "__main__":
    unittest.main()
