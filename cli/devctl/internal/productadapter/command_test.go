package productadapter

import "testing"

func TestParseAcceptsOnlyDedicatedProductGrammar(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		kind CommandKind
	}{
		{name: "prepare", args: []string{"prepare", "--count", "3", "--index", "2"}, kind: CommandPrepare},
		{name: "exec", args: []string{"exec", "--count", "3", "--index", "2", "--", "true"}, kind: CommandExec},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, err := Parse(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Kind != test.kind || command.Count != 3 || command.Index != 2 {
				t.Fatalf("parsed command = %+v", command)
			}
		})
	}
}

func TestParseRejectsAliasesDuplicatesAndAuthorityOptions(t *testing.T) {
	for _, args := range [][]string{
		{"prepare", "--count=3", "--index", "2"},
		{"prepare", "--count", "03", "--index", "2"},
		{"prepare", "--count", "+3", "--index", "2"},
		{"prepare", "--count", " 3", "--index", "2"},
		{"prepare", "--count", "3", "--index", "02"},
		{"prepare", "--index", "2", "--count", "3"},
		{"prepare", "--count", "3", "--index", "2", "--repo", ProductRepo},
		{"prepare", "--count", "3", "--count", "3"},
		{"prepare", "--count", "1", "--index", "2"},
		{"exec", "--count", "3", "--index", "2", "true"},
		{"exec", "--count", "3", "--index", "2", "--"},
		{"proxy-connect", "--count", "3", "--index", "2"},
		{"shell", "--count", "3", "--index", "2", "--", "true"},
	} {
		if command, err := Parse(args); err == nil {
			t.Fatalf("Parse(%q) = %+v, want refusal", args, command)
		}
	}
}

func TestInvocationSelectsCanonicalProductAliases(t *testing.T) {
	tests := []struct {
		name        string
		project     string
		command     string
		args        []string
		defaultRepo string
		origin      string
		paths       []string
	}{
		{name: "project", project: ProductProject},
		{name: "repo flag", project: "alias", command: "native", args: []string{"prepare", "--repo", ProductRepo}},
		{name: "canonical ssh origin", project: "alias", origin: "ssh://git@ssh.github.com:443/ouroboros-ai/ouroboros-ide.git"},
		{name: "canonical https origin", project: "alias", origin: "https://github.com/ouroboros-ai/ouroboros-ide.git"},
		{name: "owned geometry", project: "alias", paths: []string{"/workspaces/dev/agent1/ouroboros-ide"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !InvocationSelectsProduct(
				test.project,
				test.command,
				test.args,
				test.defaultRepo,
				test.origin,
				test.paths...,
			) {
				t.Fatal("canonical Product identity was not selected")
			}
		})
	}
	if InvocationSelectsProduct("devkit", "status", nil, "devkit", "ssh://git@github.com/example/devkit.git") {
		t.Fatal("unrelated source was classified as Product")
	}
}
