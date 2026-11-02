package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"

	"devkit/cli/devctl/internal/codexhistory"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func nativeHistoryCustodyRoot(stateRoot, project string) (string, error) {
	return codexhistory.CustodyRoot(stateRoot, project)
}

func nativeWorkspaceHistoryCustodyRoot(workspaceRoot, project string) (string, error) {
	return codexhistory.WorkspaceCustodyRoot(workspaceRoot, project)
}

func captureNativeSlotHistory(
	project, resetKind string,
	identity nativeSlotProcessIdentity,
	stateRoot, workspaceRoot string,
	dryRun bool,
) error {
	// A workspace projection is an additional alias, not a property of every
	// native container. Ordinary /worktrees layouts retain their explicit
	// source-declared sandbox/host homes without inventing /workspaces/dev.
	projectionRoot := ""
	if pathWithin(identity.sandboxWorktree, "/workspaces/dev") {
		projectionRoot = filepath.Dir(filepath.Clean(identity.hostWorktree))
	}
	options := codexhistory.SnapshotOptions{
		Project:                 project,
		ResetKind:               resetKind,
		AgentIndex:              identity.index,
		HostWorktree:            identity.hostWorktree,
		HostHome:                identity.hostHome,
		SandboxHome:             identity.sandboxHome,
		StateRoot:               stateRoot,
		WorkspaceRoot:           workspaceRoot,
		WorkspaceProjectionRoot: projectionRoot,
		DryRun:                  dryRun,
	}
	// New native homes are ordinary retained storage, excluded by the reset
	// planner at staging and cleanup. Validate that exact relationship before
	// omitting the cold-copy gate that still applies to homes being deleted.
	repo := filepath.Base(identity.hostWorktree)
	wantState := filepath.Join(stateRoot, fmt.Sprintf("dev-all-agent%d", identity.index))
	wantHome := filepath.Join(nativeagent.PersistentHomesRoot(stateRoot), filepath.Base(wantState), repo)
	if project == "dev-all" && identity.hostHome == wantHome {
		if identity.stateRoot != wantState {
			return fmt.Errorf("retained native history requires the source-selected slot state")
		}
		p := nativeplan.Plan{HostStateRoot: stateRoot, Agent: nativeagent.Spec{
			ID:           nativeagent.ID{Project: project, Repo: repo, Index: identity.index},
			HostWorktree: identity.hostWorktree, HostHome: identity.hostHome, StateRoot: identity.stateRoot,
		}}
		if err := launch.ValidateNativePersistentHome(p); err != nil {
			return fmt.Errorf("validate retained native history: %w", err)
		}
		// Retaining the database does not by itself retain referenced payloads.
		// Keep the existing metadata/reference and symlink checks before reset;
		// only the cold copy/import gate is inapplicable to retained storage.
		if _, err := codexhistory.Preflight(options); err != nil {
			return fmt.Errorf("validate retained native history references: %w", err)
		}
		fmt.Fprintf(os.Stderr, "codex_gui_history status=retained-in-place agent=%d home=%s\n", identity.index, identity.hostHome)
		return nil
	}
	result, err := codexhistory.Capture(options)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		os.Stderr,
		"codex_gui_history_custody schema=%s agent=%d status=%s manifest_status=%s generation=%s files=%d bytes=%d gui_rollouts=%d quarantined_gui_rollouts=%d unavailable_archived_references=%d bundle_sha256=%s manifest=%s\n",
		result.SchemaVersion,
		identity.index,
		result.Status,
		result.ManifestStatus,
		result.GenerationID,
		result.FileCount,
		result.TotalBytes,
		result.GUIRollouts,
		result.QuarantinedGUIRollouts,
		result.UnavailableArchivedReferences,
		result.BundleSHA256,
		result.ManifestPath,
	)
	if result.UnavailableGUIReferences > 0 {
		fmt.Fprintf(os.Stderr, "codex_gui_history_unavailable_gui_references schema=%s references=%d\n", result.SchemaVersion, result.UnavailableGUIReferences)
	}
	return nil
}

func captureNativeWholeHistory(
	project string,
	identities []nativeSlotProcessIdentity,
	stateRoot string,
	dryRun bool,
) error {
	for _, identity := range identities {
		if err := captureNativeSlotHistory(project, "whole-prefix-reset", identity, stateRoot, "", dryRun); err != nil {
			return fmt.Errorf("capture source-declared native slot index %d: %w", identity.index, err)
		}
	}
	return nil
}
