package plan

import (
	"fmt"
	"path/filepath"
	"strings"
)

type workspaceEgressBindSource uint8

const (
	workspaceEgressBindSourceUnregistered workspaceEgressBindSource = iota
	workspaceEgressBindSourceCanonicalTI4
)

const workspaceEgressCanonicalTI4Target = "/workspaces/dev/ti4-calculator"

// WorkspaceEgressCanonicalTI4Source is the compiled source for the one
// registered additional projection. It is a variable only so cross-package
// hermetic tests can reproduce the complete production bind set without
// depending on a particular host filesystem; no CLI or environment input can
// change it in a running devctl process.
var WorkspaceEgressCanonicalTI4Source = "/home/bayesartre/dev/control-plane-worktrees/agent1/ti4-calculator"

// workspaceEgressAdditionalBindContract is deliberately private and
// registry-backed. Native callers cannot supply an arbitrary host source or
// sandbox target.
type workspaceEgressAdditionalBindContract struct {
	Project  string
	Index    int
	Repo     string
	SourceID workspaceEgressBindSource
	Target   string
}

var workspaceEgressAdditionalBindRegistry = []workspaceEgressAdditionalBindContract{
	{
		Project:  "dev-workspace",
		Index:    2,
		Repo:     "shadow-throne-management",
		SourceID: workspaceEgressBindSourceCanonicalTI4,
		Target:   workspaceEgressCanonicalTI4Target,
	},
}

func resolveWorkspaceEgressAdditionalBinds(
	project string,
	index int,
	repo string,
	existing []Bind,
	contracts []workspaceEgressAdditionalBindContract,
) ([]Bind, error) {
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if err := validateWorkspaceEgressAdditionalBindContract(contract); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", contract.Project, contract.Index, contract.Repo, contract.Target)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf(
				"duplicate workspace-egress additional bind contract for %s-%d repo %s target %s",
				contract.Project,
				contract.Index,
				contract.Repo,
				contract.Target,
			)
		}
		seen[key] = struct{}{}
	}

	resolved := []Bind{}
	for _, contract := range contracts {
		if contract.Project != strings.TrimSpace(project) ||
			contract.Index != index ||
			contract.Repo != strings.TrimSpace(repo) {
			continue
		}
		source, err := resolveWorkspaceEgressBindSource(contract.SourceID)
		if err != nil {
			return nil, err
		}
		bind := Bind{
			Source:   source,
			Target:   contract.Target,
			Mode:     "rw",
			Required: true,
		}
		if err := validateResolvedWorkspaceEgressAdditionalBind(bind); err != nil {
			return nil, err
		}
		for _, candidate := range append(existing, resolved...) {
			if filepath.Clean(candidate.Target) == filepath.Clean(bind.Target) {
				return nil, fmt.Errorf(
					"workspace-egress additional bind target collision at %s",
					bind.Target,
				)
			}
		}
		resolved = append(resolved, bind)
	}
	return resolved, nil
}

func validateWorkspaceEgressAdditionalBindContract(contract workspaceEgressAdditionalBindContract) error {
	if contract.Project != "dev-workspace" ||
		contract.Index != 2 ||
		contract.Repo != "shadow-throne-management" {
		return fmt.Errorf(
			"unregistered workspace-egress additional bind consumer %s-%d repo %s",
			contract.Project,
			contract.Index,
			contract.Repo,
		)
	}
	if contract.SourceID != workspaceEgressBindSourceCanonicalTI4 {
		return fmt.Errorf("unregistered workspace-egress additional bind source %d", contract.SourceID)
	}
	target := strings.TrimSpace(contract.Target)
	if hasPathTraversal(target) {
		return fmt.Errorf("workspace-egress additional bind target contains traversal: %s", contract.Target)
	}
	if !filepath.IsAbs(target) || target != filepath.Clean(target) {
		return fmt.Errorf("workspace-egress additional bind target must be a clean absolute path: %s", contract.Target)
	}
	if target == "/" || target == "/workspaces" || target == "/workspaces/dev" {
		return fmt.Errorf("workspace-egress additional bind rejects broad target: %s", target)
	}
	if target != workspaceEgressCanonicalTI4Target {
		return fmt.Errorf("unregistered workspace-egress additional bind target: %s", target)
	}
	return nil
}

func resolveWorkspaceEgressBindSource(sourceID workspaceEgressBindSource) (string, error) {
	if sourceID != workspaceEgressBindSourceCanonicalTI4 {
		return "", fmt.Errorf("unregistered workspace-egress additional bind source %d", sourceID)
	}
	return WorkspaceEgressCanonicalTI4Source, nil
}

func validateResolvedWorkspaceEgressAdditionalBind(bind Bind) error {
	source := strings.TrimSpace(bind.Source)
	if hasPathTraversal(source) ||
		!filepath.IsAbs(source) ||
		source != filepath.Clean(source) {
		return fmt.Errorf("workspace-egress additional bind source must be a clean absolute path: %s", bind.Source)
	}
	if source == "/" || source == "/home" || source == "/home/bayesartre" ||
		source == "/home/bayesartre/dev" {
		return fmt.Errorf("workspace-egress additional bind rejects broad source: %s", source)
	}
	if source != WorkspaceEgressCanonicalTI4Source {
		return fmt.Errorf("unregistered workspace-egress additional bind source: %s", source)
	}
	if bind.Target != workspaceEgressCanonicalTI4Target ||
		bind.Mode != "rw" ||
		!bind.Required {
		return fmt.Errorf("workspace-egress additional bind does not match its registered projection: %#v", bind)
	}
	return nil
}

func hasPathTraversal(path string) bool {
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if segment == ".." {
			return true
		}
	}
	return false
}
