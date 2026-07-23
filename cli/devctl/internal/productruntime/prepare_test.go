package productruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/cli/devctl/internal/productadapter"
)

func TestPrepareConsumerFilesPreservesAbsentAtomicAgentRoot(t *testing.T) {
	candidate := t.TempDir()
	agentRoot := filepath.Join(candidate, "agent1")
	home := filepath.Join(candidate, "home")
	state := filepath.Join(candidate, "state")
	fixtures := t.TempDir()
	writeFixture := func(name, content string) string {
		t.Helper()
		path := filepath.Join(fixtures, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	authority := productadapter.Authority{
		Adapter: productadapter.AdapterManifest{
			CodexConfigPath:          writeFixture("config.toml", "approval_policy = \"never\"\n"),
			GovernanceRulesPath:      writeFixture("rules", "allow\n"),
			ShellHookPath:            writeFixture("bashrc", "true\n"),
			GovernanceEnvPath:        writeFixture("governance.env", "A=B\n"),
			GovernanceRepoConfigPath: writeFixture("governance.json", "{}\n"),
		},
	}
	consumer := productadapter.ConsumerManifest{
		CandidateRoot:              candidate,
		AgentRoot:                  agentRoot,
		HomePath:                   home,
		StateRoot:                  state,
		GovernanceStateRoot:        filepath.Join(state, "governance"),
		GovernanceEnvTarget:        filepath.Join(state, "governance.env"),
		GovernanceRepoConfigTarget: filepath.Join(state, "governance-repo.json"),
	}
	if err := prepareConsumerFiles(
		authority,
		consumer,
		productadapter.Command{Kind: productadapter.CommandPrepare, Count: 2, Index: 1},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(agentRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file preparation consumed the atomic source-acquisition root: %v", err)
	}
	for _, expected := range []string{
		home,
		filepath.Join(home, ".codex", "config.toml"),
		state,
		filepath.Join(state, "governance.env"),
		consumer.GovernanceStateRoot,
	} {
		if _, err := os.Lstat(expected); err != nil {
			t.Fatalf("missing prepared consumer path %s: %v", expected, err)
		}
	}
}
