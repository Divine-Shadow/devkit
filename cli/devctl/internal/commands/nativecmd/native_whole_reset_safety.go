package nativecmd

import (
	"errors"
	"fmt"
	"strings"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func validateNativeWholeResetCount(count, sourceCount int) error {
	if sourceCount < 1 {
		return fmt.Errorf("native destructive reset requires a positive source-declared agent count")
	}
	if count != sourceCount {
		return fmt.Errorf("native destructive reset count %d must equal source-declared agent count %d", count, sourceCount)
	}
	return nil
}

// sourceDerivedNativeSlotProcessIdentities derives every slot boundary from
// the same immutable plan inputs used by the native lifecycle. Whole-prefix
// reset must account for all source-declared slots, including slots without a
// currently declared GUI target.
func sourceDerivedNativeSlotProcessIdentities(opts nativeplan.BuildOptions, count int) ([]nativeSlotProcessIdentity, error) {
	if count < 1 {
		return nil, fmt.Errorf("native destructive reset requires a positive source-declared agent count")
	}
	identities := make([]nativeSlotProcessIdentity, 0, count)
	for index := 1; index <= count; index++ {
		slotOpts := opts
		slotOpts.Index = index
		plan, err := nativeplan.BuildDevAll(slotOpts)
		if err != nil {
			return nil, fmt.Errorf("derive source-declared native slot index %d: %w", index, err)
		}
		identities = append(identities, nativeSlotProcessIdentity{
			index:           index,
			hostWorktree:    plan.Agent.HostWorktree,
			sandboxWorktree: plan.Agent.SandboxWorktree,
			hostHome:        plan.Agent.HostHome,
			sandboxHome:     plan.Agent.SandboxHome,
			stateRoot:       plan.Agent.StateRoot,
			sandboxState:    plan.Agent.SandboxStateRoot,
		})
	}
	return identities, nil
}

// requireSourceDerivedNativeSlotsIdle is deliberately detection-only. A
// whole-prefix reset must not acquire, signal, or dispose a live slot; the
// selected-slot lifecycle remains the sole process-stopping reconstruction
// path. planNativeSlotProcesses supplies the exact slot identity and sibling
// protection used by that path.
func requireSourceDerivedNativeSlotsIdle(identities []nativeSlotProcessIdentity) error {
	active := make([]string, 0, len(identities))
	for _, identity := range identities {
		plan, err := planNativeSlotProcesses(identity, true)
		if err != nil {
			return fmt.Errorf("inspect source-declared native slot index %d: %w", identity.index, err)
		}
		if len(plan.pids) != 0 {
			active = append(active, fmt.Sprintf("index %d pids %v", identity.index, plan.pids))
		}
	}
	if len(active) != 0 {
		return fmt.Errorf("native destructive reset refuses active source-declared slots: %s", strings.Join(active, "; "))
	}
	return nil
}

type nativeWholeResetBoundary struct {
	inspectSlots    func() error
	lifecycleDown   func() error
	stopSessions    func()
	snapshotHistory func() error
	applyPlan       func() error
}

// executeNativeWholeResetBoundary keeps the destructive ordering explicit and
// testable. History is captured only after the post-stop scan, and a final scan
// closes the capture window before the filesystem plan may apply.
func executeNativeWholeResetBoundary(boundary nativeWholeResetBoundary) error {
	if boundary.inspectSlots == nil || boundary.lifecycleDown == nil || boundary.snapshotHistory == nil || boundary.applyPlan == nil {
		return fmt.Errorf("native destructive reset safety boundary is incomplete")
	}
	if err := boundary.inspectSlots(); err != nil {
		return err
	}
	if err := boundary.lifecycleDown(); err != nil {
		return err
	}
	if boundary.stopSessions != nil {
		boundary.stopSessions()
	}
	if err := boundary.inspectSlots(); err != nil {
		return fmt.Errorf("native destructive reset post-stop process recheck: %w", err)
	}
	if err := boundary.snapshotHistory(); err != nil {
		return fmt.Errorf("native destructive reset Codex GUI history custody: %w", err)
	}
	if err := boundary.inspectSlots(); err != nil {
		return fmt.Errorf("native destructive reset post-history process recheck: %w", err)
	}
	return boundary.applyPlan()
}

func applyNativeWholeResetFailureCleanup(identities []nativeSlotProcessIdentity, applyPlan func() error) error {
	if applyPlan == nil {
		return fmt.Errorf("native destructive reset failure cleanup plan is required")
	}
	if err := requireSourceDerivedNativeSlotsIdle(identities); err != nil {
		return fmt.Errorf("refuse cleanup after failed native reset reconstruction: %w", err)
	}
	return applyPlan()
}

// acquireNativeWholeResetLock uses the selected-slot reset's package-owned
// lock for real effects. A dry run performs no filesystem mutation and hence
// requires no cross-process exclusion.
func acquireNativeWholeResetLock(coordinationRoot, project string, dryRun bool) (*nativeSlotResetLock, error) {
	if dryRun {
		return &nativeSlotResetLock{}, nil
	}
	return acquireNativeSlotResetLock(coordinationRoot, project)
}

func withNativeWholeResetLock(coordinationRoot, project string, dryRun bool, action func() error) (retErr error) {
	if action == nil {
		return fmt.Errorf("native destructive reset locked action is required")
	}
	lock, err := acquireNativeWholeResetLock(coordinationRoot, project, dryRun)
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, lock.release())
	}()
	return action()
}
