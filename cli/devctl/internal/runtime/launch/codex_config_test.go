package launch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const projectTailBase = "# immutable selected GUI config\nmodel_provider = \"openai\"\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n[mcp_servers.governance]\ncommand = \"/nix/store/immutable-governance\"\n[projects.\"/workspaces/base\"]\ntrust_level = \"trusted\"\n"

func TestSeedCodexConfigPreservesManagedProjectTailWithoutMutation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	base := []byte(projectTailBase)
	if err := seedCodexConfigData(home, base); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".codex", "config.toml")
	want := append(append([]byte{}, base...), []byte("\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\n\n[projects.\"/workspaces/other\"]\ntrust_level = \"untrusted\"\n")...)
	if err := os.WriteFile(target, want, 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := seedCodexConfigData(home, base); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !os.SameFile(before, after) || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() {
		t.Fatal("legitimate project trust extension was mutated")
	}
}

func TestSeedCodexConfigDoesNotCreditProtectedOrUnknownDrift(t *testing.T) {
	cases := map[string]string{
		"protected-base":                      strings.Replace(projectTailBase, "approval_policy = \"never\"", "approval_policy = \"on-request\"", 1) + "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\n",
		"root-setting":                        projectTailBase + "\n[arbitrary]\nsetting = true\n",
		"mcp-drift":                           projectTailBase + "\n[mcp_servers.extra]\ncommand = \"/tmp/extra\"\n",
		"plugin-tail-without-devkit-contract": projectTailBase + "\n[plugins.\"sites@openai-bundled\"]\nenabled = true\n",
	}
	for name, observed := range cases {
		t.Run(name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			base := []byte(projectTailBase)
			if err := seedCodexConfigData(home, base); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, ".codex", "config.toml")
			if err := os.WriteFile(target, []byte(observed), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := seedCodexConfigData(home, base); err != nil {
				t.Fatal(err)
			}
			if got := readTestFile(t, target); got != projectTailBase {
				t.Fatal("out-of-contract drift received preservation credit")
			}
		})
	}
}

func TestCodexManagedProjectTrustTailClosedSyntax(t *testing.T) {
	base := []byte(projectTailBase)
	valid := map[string]string{
		"trusted":          "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\n",
		"untrusted-crlf":   "\r\n[projects.\"/workspaces/dev\"]\r\ntrust_level = \"untrusted\"\r\n",
		"toml-unicode-key": "\n[projects.\"/workspaces/\\u0064ev\"]\ntrust_level = \"trusted\"\n",
	}
	for name, tail := range valid {
		t.Run(name, func(t *testing.T) {
			if !matchesCodexManagedProjectTrustTail(append(append([]byte{}, base...), tail...), base) {
				t.Fatal("closed legitimate project tail refused")
			}
		})
	}
	invalid := map[string]string{
		"base-redeclaration":     "\n[projects.\"/workspaces/base\"]\ntrust_level = \"untrusted\"\n",
		"base-unicode-alias":     "\n[projects.\"/workspaces/\\u0062ase\"]\ntrust_level = \"untrusted\"\n",
		"duplicate-project":      "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\n[projects.\"/workspaces/\\u0064ev\"]\ntrust_level = \"untrusted\"\n",
		"duplicate-trust":        "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\ntrust_level = \"untrusted\"\n",
		"missing-trust":          "\n[projects.\"/workspaces/dev\"]\n",
		"unknown-field":          "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\nmodel = \"other\"\n",
		"unknown-trust":          "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"always\"\n",
		"relative":               "\n[projects.\"workspaces/dev\"]\ntrust_level = \"trusted\"\n",
		"unclean":                "\n[projects.\"/workspaces/../dev\"]\ntrust_level = \"trusted\"\n",
		"go-hex-escape":          "\n[projects.\"/workspaces/\\x64ev\"]\ntrust_level = \"trusted\"\n",
		"go-octal-escape":        "\n[projects.\"/workspaces/\\144ev\"]\ntrust_level = \"trusted\"\n",
		"go-bell-escape":         "\n[projects.\"/workspaces/\\a\"]\ntrust_level = \"trusted\"\n",
		"go-vtab-escape":         "\n[projects.\"/workspaces/\\v\"]\ntrust_level = \"trusted\"\n",
		"control-path":           "\n[projects.\"/workspaces/\\u0000dev\"]\ntrust_level = \"trusted\"\n",
		"invalid-unicode":        "\n[projects.\"/workspaces/\\uD800\"]\ntrust_level = \"trusted\"\n",
		"double-carriage-return": "\n[projects.\"/workspaces/dev\"]\r\r\ntrust_level = \"trusted\"\n",
		"invalid-utf8":           "\n[projects.\"/workspaces/\xff\"]\ntrust_level = \"trusted\"\n",
		"unknown-table":          "\n[arbitrary]\ntrust_level = \"trusted\"\n",
		"root-setting":           "\nmodel = \"other\"\n",
		"empty":                  "\n \t\n",
	}
	for name, tail := range invalid {
		t.Run(name, func(t *testing.T) {
			if matchesCodexManagedProjectTrustTail(append(append([]byte{}, base...), tail...), base) {
				t.Fatal("out-of-contract project tail accepted")
			}
		})
	}
}

func TestSeedCodexConfigPreservedTailRetainsModeAndNoFollowGuards(t *testing.T) {
	base := []byte(projectTailBase)
	tail := append(append([]byte{}, base...), "\n[projects.\"/workspaces/dev\"]\ntrust_level = \"trusted\"\n"...)
	t.Run("unsafe-mode", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		if err := seedCodexConfigData(home, base); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(home, ".codex", "config.toml")
		if err := os.WriteFile(target, tail, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o644); err != nil {
			t.Fatal(err)
		}
		before, err := os.Lstat(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := seedCodexConfigData(home, base); err == nil || !strings.Contains(err.Error(), "mode") {
			t.Fatal("unsafe-mode extension was credited")
		}
		after, err := os.Lstat(target)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || after.Mode().Perm() != 0o644 || readTestFile(t, target) != string(tail) {
			t.Fatal("refused unsafe-mode extension was mutated")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		if err := seedCodexConfigData(home, base); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(home, ".codex", "config.toml")
		if err := os.Rename(target, target+".original"); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, tail, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, target); err != nil {
			t.Fatal(err)
		}
		if err := seedCodexConfigData(home, base); err == nil {
			t.Fatal("symlinked extension was credited")
		}
		if readTestFile(t, outside) != string(tail) {
			t.Fatal("symlink target was mutated")
		}
	})
}
