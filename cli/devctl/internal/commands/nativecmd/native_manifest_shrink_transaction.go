package nativecmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	wtx "devkit/cli/devctl/internal/worktrees"
)

const nativeManifestShrinkTransactionSchema = "devkit/native-manifest-shrink-transaction/v1"

type nativeManifestShrinkTransactionJournal struct {
	Schema           string                            `json:"schema"`
	Status           string                            `json:"status"`
	ManifestPath     string                            `json:"manifestPath"`
	Prior            nativeagent.Manifest              `json:"prior"`
	Replacement      nativeagent.Manifest              `json:"replacement"`
	RetiredIndices   []int                             `json:"retiredIndices"`
	SlotTransactions []wtx.NativeResetTransactionState `json:"slotTransactions"`
}

func nativeManifestShrinkTransactionPath(manifestPath string) string {
	return manifestPath + ".shrink-transaction.json"
}

func readNativeManifestShrinkTransaction(path string) (bool, nativeManifestShrinkTransactionJournal, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nativeManifestShrinkTransactionJournal{}, nil
	}
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("native manifest shrink transaction %s must be a regular non-symlink file", path)
		}
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("read native manifest shrink transaction %s: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("read native manifest shrink transaction %s: invalid file descriptor", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("inspect native manifest shrink transaction %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("native manifest shrink transaction %s must be a regular non-symlink file", path)
	}
	var journal nativeManifestShrinkTransactionJournal
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("decode native manifest shrink transaction %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value")
		}
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("decode native manifest shrink transaction %s: %w", path, err)
	}
	if journal.Schema != nativeManifestShrinkTransactionSchema {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("native manifest shrink transaction %s has unsupported schema %q", path, journal.Schema)
	}
	if journal.Status != "prepared" && journal.Status != "committed" {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("native manifest shrink transaction %s has invalid status %q", path, journal.Status)
	}
	if filepath.Clean(strings.TrimSpace(journal.ManifestPath)) != journal.ManifestPath || journal.ManifestPath == "" || !filepath.IsAbs(journal.ManifestPath) {
		return false, nativeManifestShrinkTransactionJournal{}, fmt.Errorf("native manifest shrink transaction %s has unsafe manifest path", path)
	}
	return true, journal, nil
}

func writeNativeManifestShrinkTransaction(path string, journal nativeManifestShrinkTransactionJournal) error {
	if filepath.Clean(strings.TrimSpace(path)) != path || path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("native manifest shrink transaction path must be absolute and canonical")
	}
	if exists, _, err := readNativeManifestShrinkTransaction(path); err != nil {
		return err
	} else if exists {
		// The existing typed receipt is replaced atomically below.
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create native manifest shrink transaction parent: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create native manifest shrink transaction temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect native manifest shrink transaction temporary: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(journal); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode native manifest shrink transaction: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync native manifest shrink transaction temporary: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close native manifest shrink transaction temporary: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install native manifest shrink transaction: %w", err)
	}
	removeTemporary = false
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open native manifest shrink transaction parent: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync native manifest shrink transaction parent: %w", err)
	}
	return nil
}

func removeNativeManifestShrinkTransaction(path string) error {
	exists, _, err := readNativeManifestShrinkTransaction(path)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove native manifest shrink transaction %s: %w", path, err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open native manifest shrink transaction parent after removal: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync native manifest shrink transaction parent after removal: %w", err)
	}
	return nil
}

var nativeManifestShrinkBeforeCASInstall func()

func compareAndSwapNativeSlotManifest(
	path string,
	prior nativeagent.Manifest,
	replacement nativeagent.Manifest,
	dryRun bool,
) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "+ native-manifest-cas %s count=%d->%d\n", path, prior.Count, replacement.Count)
		return nil
	}
	exists, actual, err := readNativeSlotManifest(path)
	if err != nil {
		return err
	}
	if !exists || !reflect.DeepEqual(actual, prior) {
		return fmt.Errorf("shared native manifest changed before compare-and-swap")
	}
	if nativeManifestShrinkBeforeCASInstall != nil {
		nativeManifestShrinkBeforeCASInstall()
	}
	if err := nativeagent.CompareAndSwapManifest(path, prior, replacement, false); err != nil {
		return fmt.Errorf("shared native manifest changed during compare-and-swap: %w", err)
	}
	return nil
}

func nativeManifestShrinkResetOptions(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	base wtx.NativeSlotResetOptions,
	dryRun bool,
) wtx.NativeSlotResetOptions {
	return wtx.NativeSlotResetOptions{
		Project:                         manifest.Project,
		Repo:                            manifest.Repo,
		Origin:                          base.Origin,
		BranchPrefix:                    manifest.BranchPrefix,
		Index:                           spec.ID.Index,
		Count:                           manifest.Count,
		WorktreeRoot:                    manifest.HostWorktreeRoot,
		StateRoot:                       manifest.HostStateRoot,
		ProtectedRoots:                  append([]string(nil), base.ProtectedRoots...),
		GeneratedSetupLayerFiles:        append([]string(nil), base.GeneratedSetupLayerFiles...),
		DisposableGeneratedResidueRoots: append([]string(nil), base.DisposableGeneratedResidueRoots...),
		RequirePackageSourceExecutables: base.RequirePackageSourceExecutables,
		DryRun:                          dryRun,
	}
}

func planNativeManifestShrinkSurplus(
	manifest nativeagent.Manifest,
	surplus []nativeagent.Spec,
	base wtx.NativeSlotResetOptions,
	dryRun bool,
) ([]*wtx.NativeResetPlan, error) {
	plans := make([]*wtx.NativeResetPlan, 0, len(surplus))
	for _, spec := range surplus {
		plan, err := wtx.PlanNativeSlotReset(nativeManifestShrinkResetOptions(manifest, spec, base, dryRun))
		if err != nil {
			return nil, fmt.Errorf("plan surplus native slot %d retirement: %w", spec.ID.Index, err)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func nativeManifestShrinkIdentity(spec nativeagent.Spec) nativeSlotProcessIdentity {
	return nativeSlotProcessIdentity{
		index:           spec.ID.Index,
		hostWorktree:    spec.HostWorktree,
		sandboxWorktree: spec.SandboxWorktree,
		hostHome:        spec.HostHome,
		sandboxHome:     spec.SandboxHome,
		stateRoot:       spec.StateRoot,
		sandboxState:    spec.SandboxStateRoot,
	}
}

var captureNativeManifestShrinkSlotHistory = captureNativeSlotHistory

func captureNativeManifestShrinkHistory(
	manifest nativeagent.Manifest,
	surplus []nativeagent.Spec,
	dryRun bool,
) error {
	for _, spec := range surplus {
		if err := captureNativeManifestShrinkSlotHistory(
			manifest.Project,
			"manifest-shrink-retirement",
			nativeManifestShrinkIdentity(spec),
			manifest.HostStateRoot,
			"",
			dryRun,
		); err != nil {
			return fmt.Errorf("preserve surplus native slot %d Codex GUI history: %w", spec.ID.Index, err)
		}
	}
	return nil
}

func nativeManifestShrinkTransactionQuarantines(state wtx.NativeResetTransactionState) map[string]bool {
	result := make(map[string]bool, len(state.Quarantines))
	for _, quarantine := range state.Quarantines {
		result[quarantine.Path] = true
	}
	return result
}

func mergeNativeManifestShrinkTransactionStates(
	states []wtx.NativeResetTransactionState,
) wtx.NativeResetTransactionState {
	var merged wtx.NativeResetTransactionState
	for _, state := range states {
		merged.Staged = append(merged.Staged, state.Staged...)
		merged.Quarantines = append(merged.Quarantines, state.Quarantines...)
	}
	return merged
}

func requireNativeSlotManifestShrinkStagedWithPlannerAndCustody(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	state wtx.NativeResetTransactionState,
	policy nativeSlotManifestShrinkCustodyPolicy,
	planner nativeSlotManifestShrinkProcessPlanner,
) error {
	if planner == nil {
		return fmt.Errorf("surplus native slot process planner is required")
	}
	identity := nativeManifestShrinkIdentity(spec)
	inspectProcesses := func() error {
		plan, err := planner(identity, false)
		if err != nil {
			return fmt.Errorf("inspect surplus native slot %d processes: %w", spec.ID.Index, err)
		}
		if len(plan.pids) != 0 {
			return fmt.Errorf("surplus native slot %d still has active processes: %v", spec.ID.Index, plan.pids)
		}
		return nil
	}
	if err := inspectProcesses(); err != nil {
		return err
	}
	activePaths := []struct {
		name string
		path string
	}{
		{name: "app-server socket", path: filepath.Join(filepath.Clean(spec.HostHome), ".codex", fmt.Sprintf("a%d-app.sock", spec.ID.Index))},
		{name: "worktree/Git custody", path: spec.HostWorktree},
		{name: "home", path: spec.HostHome},
		{name: "state", path: spec.StateRoot},
	}
	for _, candidate := range activePaths {
		path := filepath.Clean(strings.TrimSpace(candidate.path))
		if path == "" || !filepath.IsAbs(path) {
			return fmt.Errorf("surplus native slot %d %s path must be absolute", spec.ID.Index, candidate.name)
		}
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("surplus native slot %d still has %s residue at %s", spec.ID.Index, candidate.name, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect surplus native slot %d %s %s: %w", spec.ID.Index, candidate.name, path, err)
		}
	}

	allowedQuarantines := nativeManifestShrinkTransactionQuarantines(state)
	agentRoot := filepath.Dir(filepath.Clean(spec.HostWorktree))
	agentEntries, err := os.ReadDir(agentRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enumerate staged surplus native slot %d parent %s: %w", spec.ID.Index, agentRoot, err)
	}
	for _, entry := range agentEntries {
		path := filepath.Join(agentRoot, entry.Name())
		if !allowedQuarantines[path] {
			return fmt.Errorf("surplus native slot %d parent contains non-transaction residue: %s", spec.ID.Index, path)
		}
	}
	stateParent := filepath.Dir(filepath.Clean(spec.StateRoot))
	stateEntries, err := os.ReadDir(stateParent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enumerate staged surplus native slot %d state parent %s: %w", spec.ID.Index, stateParent, err)
	}
	quarantinePrefix := fmt.Sprintf(".devkit-reset-%s-agent%d-", manifest.Project, spec.ID.Index)
	lockIdentity := fmt.Sprintf("agent%d", spec.ID.Index)
	for _, entry := range stateEntries {
		path := filepath.Join(stateParent, entry.Name())
		if strings.HasPrefix(entry.Name(), quarantinePrefix) && !allowedQuarantines[path] {
			return fmt.Errorf("surplus native slot %d has unowned state transaction residue at %s", spec.ID.Index, path)
		}
		if strings.Contains(entry.Name(), lockIdentity) && strings.HasSuffix(entry.Name(), ".lock") {
			return fmt.Errorf("surplus native slot %d has state lock residue at %s", spec.ID.Index, path)
		}
	}
	if err := requireNativeSlotManifestShrinkGitQuiescentWithQuarantines(manifest, spec, expectedOrigin, allowedQuarantines, policy); err != nil {
		return err
	}
	return inspectProcesses()
}

func requireNativeSlotManifestShrinkStagedWithPlanner(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	state wtx.NativeResetTransactionState,
	planner nativeSlotManifestShrinkProcessPlanner,
) error {
	return requireNativeSlotManifestShrinkStagedWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		state,
		nativeSlotManifestShrinkCustodyPolicy{},
		planner,
	)
}

func requireNativeSlotManifestShrinkStaged(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	state wtx.NativeResetTransactionState,
) error {
	return requireNativeSlotManifestShrinkStagedWithPlanner(manifest, spec, expectedOrigin, state, planNativeSlotProcesses)
}

func requireNativeSlotManifestShrinkStagedWithGeneratedSetup(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	state wtx.NativeResetTransactionState,
	generatedSetupLayerFiles []string,
) error {
	return requireNativeSlotManifestShrinkStagedWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		state,
		nativeSlotManifestShrinkCustodyPolicy{generatedSetupLayerFiles: generatedSetupLayerFiles},
		planNativeSlotProcesses,
	)
}

func requireNativeSlotManifestShrinkStagedWithCustody(
	manifest nativeagent.Manifest,
	spec nativeagent.Spec,
	expectedOrigin string,
	state wtx.NativeResetTransactionState,
	policy nativeSlotManifestShrinkCustodyPolicy,
) error {
	return requireNativeSlotManifestShrinkStagedWithPlannerAndCustody(
		manifest,
		spec,
		expectedOrigin,
		state,
		policy,
		planNativeSlotProcesses,
	)
}

func rollbackNativeManifestShrinkTransactions(transactions []*wtx.NativeResetTransaction) error {
	var result error
	for index := len(transactions) - 1; index >= 0; index-- {
		if err := transactions[index].Rollback(); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func commitNativeManifestShrinkTransactions(transactions []*wtx.NativeResetTransaction) error {
	var result error
	for _, transaction := range transactions {
		if err := transaction.Commit(); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func nativeManifestShrinkJournal(
	manifestPath string,
	prior nativeagent.Manifest,
	replacement nativeagent.Manifest,
	surplus []nativeagent.Spec,
) nativeManifestShrinkTransactionJournal {
	indices := make([]int, 0, len(surplus))
	for _, spec := range surplus {
		indices = append(indices, spec.ID.Index)
	}
	return nativeManifestShrinkTransactionJournal{
		Schema:         nativeManifestShrinkTransactionSchema,
		Status:         "prepared",
		ManifestPath:   manifestPath,
		Prior:          prior,
		Replacement:    replacement,
		RetiredIndices: indices,
	}
}

func validateNativeManifestShrinkJournal(
	journal nativeManifestShrinkTransactionJournal,
	manifestPath string,
	expected nativeagent.Manifest,
	canonicalPrior nativeagent.Manifest,
) ([]nativeagent.Spec, error) {
	if journal.ManifestPath != manifestPath {
		return nil, fmt.Errorf("native manifest shrink recovery names a different manifest path")
	}
	if !reflect.DeepEqual(journal.Replacement, expected) {
		return nil, fmt.Errorf("native manifest shrink recovery replacement disagrees with current source-derived manifest")
	}
	surplus, err := classifyNativeSlotManifestShrink(journal.Prior, journal.Replacement, canonicalPrior)
	if err != nil {
		return nil, fmt.Errorf("native manifest shrink recovery has invalid prior geometry: %w", err)
	}
	indices := make([]int, 0, len(surplus))
	for _, spec := range surplus {
		indices = append(indices, spec.ID.Index)
	}
	if !reflect.DeepEqual(indices, journal.RetiredIndices) {
		return nil, fmt.Errorf("native manifest shrink recovery retired-index identity mismatch")
	}
	if len(journal.SlotTransactions) != len(surplus) {
		return nil, fmt.Errorf("native manifest shrink recovery slot transaction count mismatch")
	}
	return surplus, nil
}

func recoverNativeManifestShrinkTransaction(
	manifestPath string,
	current nativeagent.Manifest,
	expected nativeagent.Manifest,
	buildOptions nativeplan.BuildOptions,
	resetOptions wtx.NativeSlotResetOptions,
	dryRun bool,
	custodyPolicy nativeSlotManifestShrinkCustodyPolicy,
) (bool, error) {
	transactionPath := nativeManifestShrinkTransactionPath(manifestPath)
	exists, journal, err := readNativeManifestShrinkTransaction(transactionPath)
	if err != nil || !exists {
		return false, err
	}
	if dryRun {
		return false, fmt.Errorf("native manifest shrink has a pending typed recovery transaction at %s", transactionPath)
	}
	canonicalPrior, err := nativeplan.BuildManifest(buildOptions, journal.Prior.Count)
	if err != nil {
		return false, fmt.Errorf("derive canonical prior manifest for shrink recovery: %w", err)
	}
	surplus, err := validateNativeManifestShrinkJournal(journal, manifestPath, expected, canonicalPrior)
	if err != nil {
		return false, err
	}
	plans, err := planNativeManifestShrinkSurplus(journal.Prior, surplus, resetOptions, false)
	if err != nil {
		return false, err
	}
	transactions := make([]*wtx.NativeResetTransaction, 0, len(plans))
	for index, plan := range plans {
		transaction, err := wtx.ResumeNativeResetTransaction(plan, journal.SlotTransactions[index])
		if err != nil {
			return false, fmt.Errorf("resume surplus native slot %d retirement: %w", surplus[index].ID.Index, err)
		}
		transactions = append(transactions, transaction)
	}
	switch {
	case reflect.DeepEqual(current, journal.Replacement):
		if err := commitNativeManifestShrinkTransactions(transactions); err != nil {
			return false, fmt.Errorf("resume post-CAS surplus native slot cleanup; typed recovery retained at %s: %w", transactionPath, err)
		}
		for _, spec := range surplus {
			if err := requireNativeSlotManifestShrinkStagedWithCustody(journal.Prior, spec, resetOptions.Origin, wtx.NativeResetTransactionState{}, custodyPolicy); err != nil {
				return false, fmt.Errorf("verify recovered surplus native slot %d absence: %w", spec.ID.Index, err)
			}
		}
		if err := removeNativeManifestShrinkTransaction(transactionPath); err != nil {
			return false, err
		}
		return true, nil
	case reflect.DeepEqual(current, journal.Prior):
		if journal.Status == "committed" {
			return false, fmt.Errorf("committed native manifest shrink transaction has a reverted manifest")
		}
		if err := rollbackNativeManifestShrinkTransactions(transactions); err != nil {
			return false, fmt.Errorf("roll back prepared native manifest shrink transaction; typed recovery retained at %s: %w", transactionPath, err)
		}
		if err := removeNativeManifestShrinkTransaction(transactionPath); err != nil {
			return false, err
		}
		return false, nil
	default:
		return false, fmt.Errorf("native manifest shrink recovery found a manifest outside both typed CAS states")
	}
}

func nativeManifestShrinkIndices(surplus []nativeagent.Spec) string {
	values := make([]string, 0, len(surplus))
	for _, spec := range surplus {
		values = append(values, strconv.Itoa(spec.ID.Index))
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
