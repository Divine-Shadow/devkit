package nativecmd

import (
	"fmt"
	"os"

	"devkit/cli/devctl/internal/codexhistory"
)

func nativeHistoryCustodyRoot(stateRoot, project string) (string, error) {
	return codexhistory.CustodyRoot(stateRoot, project)
}

func captureNativeSlotHistory(
	project, resetKind string,
	identity nativeSlotProcessIdentity,
	stateRoot string,
	dryRun bool,
) error {
	result, err := codexhistory.Capture(codexhistory.SnapshotOptions{
		Project:     project,
		ResetKind:   resetKind,
		AgentIndex:  identity.index,
		HostHome:    identity.hostHome,
		SandboxHome: identity.sandboxHome,
		StateRoot:   stateRoot,
		DryRun:      dryRun,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(
		os.Stderr,
		"codex_gui_history_custody schema=%s agent=%d status=%s generation=%s files=%d bytes=%d gui_rollouts=%d bundle_sha256=%s manifest=%s\n",
		codexhistory.SnapshotSchema,
		identity.index,
		result.Status,
		result.GenerationID,
		result.FileCount,
		result.TotalBytes,
		result.GUIRollouts,
		result.BundleSHA256,
		result.ManifestPath,
	)
	return nil
}

func captureNativeWholeHistory(
	project string,
	identities []nativeSlotProcessIdentity,
	stateRoot string,
	dryRun bool,
) error {
	for _, identity := range identities {
		if err := captureNativeSlotHistory(project, "whole-prefix-reset", identity, stateRoot, dryRun); err != nil {
			return fmt.Errorf("capture source-declared native slot index %d: %w", identity.index, err)
		}
	}
	return nil
}
