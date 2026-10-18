import importlib.util
import pathlib
import sys
import tempfile
import unittest

script = sys.argv.pop()
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("binding", script)
binding = importlib.util.module_from_spec(spec)
spec.loader.exec_module(binding)


class SourceBindingTest(unittest.TestCase):
    def fixture(self, base):
        root = base / "source"
        for name in binding.CONTROL_FILES + ["main-actions/src/main/scala/Example.scala"]:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("public fixed source\n")
        return root

    def test_exact_sources_are_copied_and_later_mutation_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            root = self.fixture(base)
            output = base / "binding"
            name = "main-actions/src/main/scala/Example.scala"
            (root / name).chmod(0o755)
            binding.capture(root, output)
            binding.check(root, output)
            self.assertEqual((output / "sources" / name).read_bytes(), (root / name).read_bytes())
            self.assertTrue((output / "sources" / name).stat().st_mode & 0o111)
            (root / name).write_text("unexpected source mutation")
            with self.assertRaisesRegex(ValueError, "effective source changed"):
                binding.check(root, output)

    def test_added_source_changes_the_binding(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            root = self.fixture(base)
            binding.capture(root, base / "binding")
            (root / "main-actions/src/main/scala/Extra.scala").write_text("extra")
            with self.assertRaisesRegex(ValueError, "effective source changed"):
                binding.check(root, base / "binding")

    def test_source_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            root = self.fixture(base)
            (root / "main-actions/src/main/scala/Escape.scala").symlink_to("/outside")
            with self.assertRaisesRegex(ValueError, "source symlink"):
                binding.capture(root, base / "binding")

    def test_generated_time_requires_the_fixed_epoch(self):
        with tempfile.TemporaryDirectory() as directory:
            base = pathlib.Path(directory)
            root = self.fixture(base)
            path = root / "target/resource_managed/xsbt.version.properties"
            path.parent.mkdir(parents=True)
            path.write_text("version=2.0.0-RC8\ntimestamp=19700101T000000\n")
            binding.check_generated(root, 0, base / "generated.json")
            path.write_text("version=2.0.0-RC8\ntimestamp=20261018T235112\n")
            with self.assertRaisesRegex(ValueError, "differs from fixed source epoch"):
                binding.check_generated(root, 0, base / "generated.json")


unittest.main()
