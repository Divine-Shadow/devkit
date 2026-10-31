package nativecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func TestCurrentSourceRetainsSelectedConsumerHelperAcrossSystemChange(t *testing.T) {
	store := t.TempDir()
	makeRuntime := func(name string) string {
		root := filepath.Join(store, name)
		helper := filepath.Join(root, "kit", "bin", "devctl")
		if err := os.MkdirAll(filepath.Dir(helper), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0555); err != nil {
			t.Fatal(err)
		}
		return root
	}
	preparer := makeRuntime("new-preparer")
	retained := makeRuntime("retained-launcher")
	current := makeRuntime("different-current-system")
	consumer := nativeplan.GUIConsumerWorkspace{TargetID: "shadow-throne-local-2", Worktree: "/workspaces/dev/ouroboros-ide", Home: "/workspaces/dev/.devhome-agent2"}
	helper, err := resolveCurrentSourceConsumerHelperFrom(preparer, retained, consumer.TargetID, consumer, store)
	if err != nil || helper != filepath.Join(retained, "kit", "bin", "devctl") {
		t.Fatalf("retained consumer helper %q: %v", helper, err)
	}
	q := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	hostRoot := "/home/bayesartre/dev"
	command := "package-managed-ssh -o " + q("ProxyCommand="+q(helper)+" -p 'dev-all' native proxy-connect --socket '"+hostRoot+"/.devkit/native-egress/.managed-egress-123.sock' --target %h:%p")
	if err := validateCurrentSourceTransport("package-managed-ssh", helper, hostRoot, command); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []string{preparer, current} {
		if err := validateCurrentSourceTransport("package-managed-ssh", filepath.Join(wrong, "kit", "bin", "devctl"), hostRoot, command); err == nil {
			t.Fatal("accepted helper outside the selected live consumer")
		}
	}
	for name, bad := range map[string]string{
		"foreign socket root": strings.ReplaceAll(command, hostRoot, "/foreign"),
		"noncanonical socket": strings.ReplaceAll(command, "/native-egress/", "/native-egress/../native-egress/"),
		"nonlauncher socket":  strings.ReplaceAll(command, "managed-egress-123", "managed-egress-0"),
		"command suffix":      command + "; replacement",
	} {
		if err := validateCurrentSourceTransport("package-managed-ssh", helper, hostRoot, bad); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	for name, binding := range map[string][2]string{
		"foreign consumer":     {retained, "darksteel-1"},
		"missing target":       {retained, ""},
		"missing runtime":      {"", consumer.TargetID},
		"noncanonical runtime": {retained + "/../retained-launcher", consumer.TargetID},
		"missing helper":       {filepath.Join(store, "missing"), consumer.TargetID},
		"foreign store":        {t.TempDir(), consumer.TargetID},
	} {
		if _, err := resolveCurrentSourceConsumerHelperFrom(preparer, binding[0], binding[1], consumer, store); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	for name, setup := range map[string]func(string){
		"symlinked helper": func(path string) {
			if err := os.Symlink(filepath.Join(retained, "kit", "bin", "devctl"), path); err != nil {
				t.Fatal(err)
			}
		},
		"nonexecutable helper": func(path string) {
			if err := os.WriteFile(path, []byte("not executable"), 0444); err != nil {
				t.Fatal(err)
			}
		},
		"directory helper": func(path string) {
			if err := os.Mkdir(path, 0555); err != nil {
				t.Fatal(err)
			}
		},
	} {
		root := filepath.Join(store, strings.ReplaceAll(name, " ", "-"))
		path := filepath.Join(root, "kit", "bin", "devctl")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		setup(path)
		if _, err := resolveCurrentSourceConsumerHelperFrom(preparer, root, consumer.TargetID, consumer, store); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	t.Setenv("DEVKIT_CURRENT_SOURCE_CONSUMER_RUNTIME_ROOT", current)
	t.Setenv("DEVKIT_CURRENT_SOURCE_CONSUMER_TARGET_ID", consumer.TargetID)
	helper, err = resolveCurrentSourceConsumerHelperFrom(preparer, "", "", consumer, store)
	if err != nil || helper != filepath.Join(preparer, "kit", "bin", "devctl") {
		t.Fatalf("unbound same-package consumer: %q %v", helper, err)
	}
}
