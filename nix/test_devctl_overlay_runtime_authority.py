#!/usr/bin/env python3
"""Light argv/refusal tests: owned executables, no Nix build or network."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "kit/scripts/devctl-overlay-runtime-authority"


class AuthorityCommandTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.calls = self.root / 'calls.jsonl'
        self.fake = self.root / 'selected nix with spaces'
        self.executable(self.fake, '''import json,os,pathlib,sys
r=pathlib.Path(os.environ['OWNED_ARGV_FIXTURE'])
with (r/'calls.jsonl').open('a') as f:
 f.write(json.dumps({'argv':sys.argv[1:],'git':os.environ.get('GIT_SSH_COMMAND'),'aws':os.environ.get('AWS_SHARED_CREDENTIALS_FILE')})+'\\n')
if os.environ.get('OWNED_BAD_PACKAGE'):
 print(r/'invalid-package');sys.exit(0)
sys.exit(73)
''')
        self.prefix = ['--option', 'flake-registry', '', '--option', 'max-jobs', '1', '--option', 'cores', '2', '--option', 'timeout', '1200', '--option', 'max-silent-time', '300']
        self.env = dict(os.environ, OWNED_ARGV_FIXTURE=str(self.root), GIT_SSH_COMMAND='owned-connector-sentinel', AWS_SHARED_CREDENTIALS_FILE='owned-credential-sentinel')
        self.env.pop('DEVKIT_DEVCTL_PACKAGE', None)

    def executable(self, path, source):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('#!' + sys.executable + '\n' + source)
        path.chmod(0o755)

    def run_helper(self, args=None, extra_env=None):
        return subprocess.run(['bash', str(SCRIPT), *(args if args is not None else ['--', str(self.fake), *self.prefix])], cwd=ROOT, env=dict(self.env, **(extra_env or {})), capture_output=True, text=True, timeout=5)

    def recorded(self):
        return [json.loads(line) for line in self.calls.read_text().splitlines()]

    def test_selected_argv_preserves_resources_empty_value_and_subject_environment(self):
        result = self.run_helper()
        self.assertEqual(result.returncode, 73)
        calls = self.recorded()
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]['argv'], self.prefix + ['build', '--no-link', '--print-out-paths', str(ROOT) + '#devctl'])
        self.assertEqual(calls[0]['git'], 'owned-connector-sentinel')
        self.assertEqual(calls[0]['aws'], 'owned-credential-sentinel')
        self.assertNotIn('devctl-overlay-runtime-authority: ok', result.stdout)

    def test_literal_shell_text_is_not_evaluated(self):
        marker = self.root / 'must-not-exist'
        literal = '$(touch ' + str(marker) + '); `false`'
        result = self.run_helper(['--', str(self.fake), *self.prefix, '--owned-literal', literal])
        self.assertEqual(result.returncode, 73)
        self.assertEqual(self.recorded()[0]['argv'][len(self.prefix):len(self.prefix)+2], ['--owned-literal', literal])
        self.assertFalse(marker.exists())

    def test_explicit_missing_command_never_falls_back(self):
        bin_dir = self.root / 'poison-bin'
        self.executable(bin_dir / 'nix', "import pathlib,os; pathlib.Path(os.environ['OWNED_ARGV_FIXTURE'],'fallback-called').touch(); raise SystemExit(95)\n")
        result = self.run_helper(['--', str(self.root / 'missing')], {'PATH': str(bin_dir) + ':' + self.env['PATH']})
        self.assertEqual(result.returncode, 127)
        self.assertFalse((self.root / 'fallback-called').exists())
        self.assertFalse(self.calls.exists())

    def test_invalid_explicit_invocations_refuse_before_execution(self):
        for args in (['--'], ['--', ''], ['unexpected']):
            with self.subTest(args=args):
                self.assertEqual(self.run_helper(args).returncode, 64)
        self.assertFalse(self.calls.exists())

    def test_standalone_default_keeps_original_command(self):
        bin_dir = self.root / 'default-bin'
        bin_dir.mkdir()
        (bin_dir / 'nix').symlink_to(self.fake)
        result = self.run_helper([], {'PATH': str(bin_dir) + ':' + self.env['PATH']})
        self.assertEqual(result.returncode, 73)
        self.assertEqual(self.recorded()[0]['argv'][:2], ['--extra-experimental-features', 'nix-command flakes'])

    def test_malformed_package_retains_existing_assertions(self):
        result = self.run_helper(extra_env={'OWNED_BAD_PACKAGE': '1'})
        self.assertEqual(result.returncode, 1)
        self.assertEqual(len(self.recorded()), 1)
        self.assertNotIn('devctl-overlay-runtime-authority: ok', result.stdout)


if __name__ == '__main__':
    unittest.main()
