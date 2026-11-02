package nativecmd

import (
	"devkit/cli/devctl/internal/cmdregistry"
	"testing"
)

func TestRetiredFleetSelectorsRefuseBeforePreparation(t *testing.T) {
	for _, args := range [][]string{
		{"prepare", "--index", "5", "--controller-lifecycle-lease-fd", "3"},
		{"exec", "--software-only", "--", "true"},
		{"prepare", "--preparation-phase", "tf1-tools-provider-deferred"},
		{"readiness", "--preparation-phase", "tf1-tools-provider-deferred"},
		{"exec", "--preparation-phase", "tf1-tools-provider-deferred", "--", "true"},
	} {
		if _, err := parsePlanArgs(&cmdregistry.Context{Project: "dev-all", Args: args}, args[0] == "exec", false); err == nil {
			t.Fatalf("retired selector accepted: %v", args)
		}
	}
	for _, selector := range []string{"--software-only", "--preparation-phase"} {
		if _, err := parseTopExecArgs(&cmdregistry.Context{Project: "dev-all", Args: []string{"1", selector, "--", "true"}}, false); err == nil {
			t.Fatalf("retired top-level selector accepted: %s", selector)
		}
	}
}
