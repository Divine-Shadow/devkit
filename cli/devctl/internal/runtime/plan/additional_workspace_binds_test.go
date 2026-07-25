package plan

import (
	"path/filepath"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/devkitpaths"
)

func TestDevWorkspaceTwoReceivesOnlyRegisteredTI4AdditionalBind(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	hostWorktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")

	p, err := Build(BuildOptions{
		Paths:            devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit")},
		HostRoot:         devRoot,
		WorktreeRoot:     hostWorktreeRoot,
		Project:          "dev-workspace",
		Index:            2,
		Repo:             "shadow-throne-management",
		IsolationProfile: IsolationProfileWorkspaceEgress,
		EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := Bind{
		Source:   WorkspaceEgressCanonicalTI4Source,
		Target:   workspaceEgressCanonicalTI4Target,
		Mode:     "rw",
		Required: true,
	}
	if !hasExactBind(p.Binds, want) {
		t.Fatalf("missing exact registered additional bind %#v in %#v", want, p.Binds)
	}
	if !hasBind(p.Binds, p.Agent.HostWorktree, "/workspace") {
		t.Fatalf("existing Management /workspace bind changed: %#v", p.Binds)
	}
	rendered := strings.Join(p.LauncherArgs, "\x00")
	for _, part := range []string{"--bind", WorkspaceEgressCanonicalTI4Source, workspaceEgressCanonicalTI4Target} {
		if !strings.Contains(rendered, part) {
			t.Fatalf("launcher args missing %q: %#v", part, p.LauncherArgs)
		}
	}
	for _, bind := range p.Binds {
		if bind.Source == devRoot || bind.Target == "/workspaces/dev" {
			t.Fatalf("additional contract exposed broad dev root: %#v", bind)
		}
	}
}

func TestWorkspaceEgressAdditionalBindDoesNotChangeExistingTargets(t *testing.T) {
	root := t.TempDir()
	devRoot := filepath.Join(root, "dev")
	hostWorktreeRoot := filepath.Join(devRoot, "control-plane-worktrees")
	cases := []struct {
		name         string
		project      string
		index        int
		repo         string
		dedicatedTI4 bool
	}{
		{name: "other Management index", project: "dev-workspace", index: 1, repo: "shadow-throne-management"},
		{name: "other project", project: "dev-all", index: 2, repo: "shadow-throne-management"},
		{name: "other repository", project: "dev-workspace", index: 2, repo: "ouroboros-ide"},
		{name: "existing dedicated TI4 target", project: "dev-workspace", index: 1, repo: "ti4-calculator", dedicatedTI4: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			p, err := Build(BuildOptions{
				Paths:            devkitpaths.Paths{Root: filepath.Join(devRoot, "devkit")},
				HostRoot:         devRoot,
				WorktreeRoot:     hostWorktreeRoot,
				Project:          testCase.project,
				Index:            testCase.index,
				Repo:             testCase.repo,
				IsolationProfile: IsolationProfileWorkspaceEgress,
				EgressAllowlist:  filepath.Join(root, "allowlist.txt"),
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if hasBind(p.Binds, WorkspaceEgressCanonicalTI4Source, workspaceEgressCanonicalTI4Target) {
				t.Fatalf("non-target consumer received the registered Management bind: %#v", p.Binds)
			}
			if testCase.dedicatedTI4 {
				dedicatedSource := filepath.Join(hostWorktreeRoot, "agent1", "ti4-calculator")
				if !hasBind(p.Binds, dedicatedSource, workspaceEgressCanonicalTI4Target) {
					t.Fatalf("existing dedicated TI4 alias changed: %#v", p.Binds)
				}
			}
		})
	}
}

func TestWorkspaceEgressAdditionalBindContractsFailClosed(t *testing.T) {
	valid := workspaceEgressAdditionalBindRegistry[0]
	tests := []struct {
		name      string
		contracts []workspaceEgressAdditionalBindContract
		existing  []Bind
	}{
		{
			name: "broad target",
			contracts: []workspaceEgressAdditionalBindContract{
				withAdditionalBindTarget(valid, "/workspaces/dev"),
			},
		},
		{
			name: "traversal target",
			contracts: []workspaceEgressAdditionalBindContract{
				withAdditionalBindTarget(valid, "/workspaces/dev/sibling/../ti4-calculator"),
			},
		},
		{
			name: "unregistered target",
			contracts: []workspaceEgressAdditionalBindContract{
				withAdditionalBindTarget(valid, "/workspaces/dev/other"),
			},
		},
		{
			name: "unregistered source",
			contracts: []workspaceEgressAdditionalBindContract{
				withAdditionalBindSource(valid, workspaceEgressBindSourceUnregistered),
			},
		},
		{
			name:      "duplicate contract",
			contracts: []workspaceEgressAdditionalBindContract{valid, valid},
		},
		{
			name:      "target collision",
			contracts: []workspaceEgressAdditionalBindContract{valid},
			existing: []Bind{{
				Source:   "/registered/other",
				Target:   workspaceEgressCanonicalTI4Target,
				Mode:     "rw",
				Required: true,
			}},
		},
		{
			name: "unregistered consumer",
			contracts: []workspaceEgressAdditionalBindContract{
				withAdditionalBindIndex(valid, 3),
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := resolveWorkspaceEgressAdditionalBinds(
				"dev-workspace",
				2,
				"shadow-throne-management",
				testCase.existing,
				testCase.contracts,
			)
			if err == nil {
				t.Fatal("unsafe additional bind contract must fail closed")
			}
		})
	}
}

func TestWorkspaceEgressAdditionalBindRejectsBroadSourceRoot(t *testing.T) {
	err := validateResolvedWorkspaceEgressAdditionalBind(Bind{
		Source:   "/home/bayesartre/dev",
		Target:   workspaceEgressCanonicalTI4Target,
		Mode:     "rw",
		Required: true,
	})
	if err == nil {
		t.Fatal("broad source root must fail closed")
	}
}

func withAdditionalBindTarget(
	contract workspaceEgressAdditionalBindContract,
	target string,
) workspaceEgressAdditionalBindContract {
	contract.Target = target
	return contract
}

func withAdditionalBindSource(
	contract workspaceEgressAdditionalBindContract,
	source workspaceEgressBindSource,
) workspaceEgressAdditionalBindContract {
	contract.SourceID = source
	return contract
}

func withAdditionalBindIndex(
	contract workspaceEgressAdditionalBindContract,
	index int,
) workspaceEgressAdditionalBindContract {
	contract.Index = index
	return contract
}
