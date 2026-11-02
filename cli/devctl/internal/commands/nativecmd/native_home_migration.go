package nativecmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	"devkit/cli/devctl/internal/devkitpaths"
	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	"devkit/cli/devctl/internal/runtime/launch"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// Compiled from the same source-owned Nix recipe as the native CLI. There is
// no ambient executable, shell, source/destination or copy fallback selector.
var nativeHomeMoveExecutable string

type nativeHomeMigrationResult struct {
	Status      string `json:"status"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
}

func handleNativeHomeMigration(ctx *cmdregistry.Context) error {
	flags := flag.NewFlagSet("native migrate-home", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo", "", "source-declared repository")
	index := flags.Int("index", 0, "source-declared native slot")
	if err := flags.Parse(ctx.Args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || ctx.Project != "dev-all" || ctx.Profile != "" || *repo != "ouroboros-ide" || *index < 1 {
		return fmt.Errorf("native migrate-home requires dev-all --repo ouroboros-ide --index N without geometry or profile overrides")
	}
	authority := ctx.Paths.RuntimeAuthorityRoot
	if !filepath.IsAbs(authority) || filepath.Clean(authority) != authority || !strings.HasPrefix(authority, "/nix/store/") {
		return fmt.Errorf("native home migration requires executable-owned immutable runtime authority")
	}
	owned := *ctx
	owned.Paths = devkitpaths.Paths{Root: authority, Kit: filepath.Join(authority, "kit"), RuntimeAuthorityRoot: authority, OverlayPaths: []string{filepath.Join(authority, "overlays")}}
	cfg, _, err := config.ReadAll(owned.Paths.OverlayPaths, owned.Project)
	if err != nil {
		return err
	}
	admitted := *index <= cfg.Defaults.Agents
	for _, selected := range cfg.Defaults.SelectedAgents {
		admitted = admitted || selected == *index
	}
	if !admitted {
		return fmt.Errorf("native home migration index is not source-declared")
	}
	parsed, err := parsePlanArgs(&owned, false, false)
	if err != nil {
		return err
	}
	if err := applyNativeConfigDefaults(&owned, cfg, &parsed.opts); err != nil {
		return err
	}
	paths, err := nativeagent.ResolvePaths(nativeagent.PathConfig{DevkitRoot: authority, HostRoot: cfg.Native.HostRoot, Project: owned.Project, Repo: *repo, Index: *index, WorktreeRoot: parsed.opts.WorktreeRoot, StateRoot: parsed.opts.StateRoot, DedicatedWorktree: true})
	if err != nil {
		return err
	}
	parsed.opts.WorkspaceRoot = filepath.Dir(paths.HostWorktree)
	p, err := nativeplan.BuildDevAll(parsed.opts)
	if err != nil {
		return err
	}
	coordination, err := nativeSlotResetCoordinationRoot(parsed.opts.WorktreeRoot)
	if err != nil {
		return err
	}
	if !ctx.DryRun {
		lock, err := acquireNativeSlotResetLock(coordination, owned.Project)
		if err != nil {
			return err
		}
		defer lock.release()
	}
	inspectIdle := func() error {
		for _, home := range []string{p.Agent.HostHome, nativeagent.PersistentDevAllHome(p.Agent.HostWorktree, *index), filepath.Join(p.Agent.StateRoot, "home")} {
			identity := nativeManifestShrinkIdentity(p.Agent)
			identity.hostHome = home
			processes, err := planNativeSlotProcesses(identity, true)
			if err != nil {
				return err
			}
			if len(processes.pids) != 0 {
				return fmt.Errorf("native home migration refuses active slot processes: %v", processes.pids)
			}
			socket := filepath.Join(home, ".codex", fmt.Sprintf("a%d-app.sock", *index))
			if _, err := os.Lstat(socket); !os.IsNotExist(err) {
				return fmt.Errorf("native home migration requires owning lifecycle disposition for app-server endpoint %s", socket)
			}
		}
		return nil
	}
	result, err := migrateNativeHome(p, ctx.DryRun, inspectIdle)
	encoded, marshalErr := json.Marshal(result)
	if marshalErr == nil {
		fmt.Fprintln(os.Stdout, string(encoded))
	}
	return errors.Join(err, marshalErr)
}

func migrateNativeHome(p nativeplan.Plan, dryRun bool, inspectIdle func() error) (nativeHomeMigrationResult, error) {
	result := nativeHomeMigrationResult{Destination: p.Agent.HostHome, Status: "refused"}
	if inspectIdle == nil {
		return result, fmt.Errorf("native home migration requires quiescent lineage inspection")
	}
	if p.Agent.ID.Project != "dev-all" || p.Agent.ID.Index < 1 || nativeagent.IsWorkspaceRootRepo(p.Agent.ID.Repo) || filepath.Base(p.Agent.ID.Repo) != p.Agent.ID.Repo || p.Agent.ID.Repo == ".." || !filepath.IsAbs(p.HostStateRoot) || filepath.Clean(p.HostStateRoot) != p.HostStateRoot || filepath.Dir(p.Agent.StateRoot) != p.HostStateRoot ||
		p.Agent.HostHome != filepath.Join(nativeagent.PersistentHomesRoot(p.HostStateRoot), filepath.Base(p.Agent.StateRoot), p.Agent.ID.Repo) {
		return result, fmt.Errorf("native home migration requires exact source-selected persistent geometry")
	}
	legacy := ""
	for _, path := range []string{nativeagent.PersistentDevAllHome(p.Agent.HostWorktree, p.Agent.ID.Index), filepath.Join(p.Agent.StateRoot, "home")} {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return result, err
		}
		if legacy != "" {
			return result, fmt.Errorf("multiple legacy homes require an owning data disposition; no merge was performed")
		}
		legacy = path
	}
	if legacy == "" {
		if err := launch.ValidateNativePersistentHome(p); err != nil {
			return result, err
		}
		result.Status = "no-legacy-home"
		return result, nil
	}
	result.Source = legacy
	if _, err := os.Lstat(p.Agent.HostHome); !os.IsNotExist(err) {
		return result, fmt.Errorf("persistent home destination is occupied or inaccessible; no overwrite or merge was performed")
	}
	sourceParent, err := openFreshPreparationDirectory(filepath.Dir(legacy), false)
	if err != nil {
		return result, err
	}
	defer sourceParent.Close()
	fd, err := syscall.Openat(int(sourceParent.Fd()), filepath.Base(legacy), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return result, fmt.Errorf("open exact legacy home without redirection: %w", err)
	}
	source := os.NewFile(uintptr(fd), legacy)
	defer source.Close()
	before, err := source.Stat()
	if err != nil {
		return result, err
	}
	owner, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.IsDir() || owner.Uid != uint32(os.Geteuid()) || before.Mode().Perm() != 0700 {
		return result, fmt.Errorf("legacy home must be a private effective-user-owned directory")
	}
	if err := inspectIdle(); err != nil {
		return result, err
	}
	if dryRun {
		result.Status = "planned"
		return result, nil
	}
	destinationParent, err := openFreshPreparationDirectory(filepath.Dir(p.Agent.HostHome), true)
	if err != nil {
		return result, err
	}
	defer destinationParent.Close()
	for _, path := range []string{nativeagent.PersistentHomesRoot(p.HostStateRoot), filepath.Dir(p.Agent.HostHome)} {
		if _, err := currentPersistentHomeDirectory(path); err != nil {
			return result, err
		}
	}
	validateParents := func() error {
		for _, parent := range []*os.File{sourceParent, destinationParent} {
			current, err := openFreshPreparationDirectory(parent.Name(), false)
			if err != nil {
				return err
			}
			currentInfo, currentErr := current.Stat()
			current.Close()
			heldInfo, heldErr := parent.Stat()
			if currentErr != nil || heldErr != nil || !os.SameFile(currentInfo, heldInfo) {
				return fmt.Errorf("native home migration parent identity changed")
			}
		}
		current, err := os.Lstat(legacy)
		if err != nil || !os.SameFile(before, current) {
			return fmt.Errorf("legacy home identity changed before migration")
		}
		return nil
	}
	if err := validateParents(); err != nil {
		return result, err
	}
	if err := inspectIdle(); err != nil {
		return result, err
	}
	if err := validateParents(); err != nil {
		return result, err
	}
	moveErr := moveNativeHomeNoReplace(sourceParent, filepath.Base(legacy), destinationParent, filepath.Base(p.Agent.HostHome))
	after, afterErr := os.Lstat(p.Agent.HostHome)
	_, sourceErr := os.Lstat(legacy)
	if afterErr == nil && os.SameFile(before, after) && os.IsNotExist(sourceErr) {
		result.Status = "moved"
		if syncErr := errors.Join(sourceParent.Sync(), destinationParent.Sync()); syncErr != nil {
			result.Status = "moved-durability-unconfirmed"
			return result, errors.Join(moveErr, syncErr)
		}
		return result, moveErr
	}
	current, currentErr := os.Lstat(legacy)
	if currentErr == nil && os.SameFile(before, current) {
		return result, errors.Join(moveErr, fmt.Errorf("native home was not moved; original remains intact"))
	}
	result.Status = "effect-unknown"
	return result, errors.Join(moveErr, fmt.Errorf("native home migration disposition is unknown; retain both namespaces and do not retry"))
}

func currentPersistentHomeDirectory(path string) (os.FileInfo, error) {
	file, err := openFreshPreparationDirectory(path, false)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("persistent home parent must be private and effective-user-owned")
	}
	return info, nil
}

func moveNativeHomeNoReplace(source *os.File, sourceName string, destination *os.File, destinationName string) error {
	binary, err := filepath.EvalSymlinks(nativeHomeMoveExecutable)
	if err != nil || !filepath.IsAbs(nativeHomeMoveExecutable) || filepath.Clean(nativeHomeMoveExecutable) != nativeHomeMoveExecutable || !strings.HasPrefix(binary, "/nix/store/") || filepath.Base(nativeHomeMoveExecutable) != "mv" {
		return fmt.Errorf("native home migration requires the compiled immutable no-copy move executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Preserve the compiled mv entry name: coreutils can resolve that symlink
	// to its multicall binary, whose argv[0] still selects the mv operation.
	cmd := exec.CommandContext(ctx, nativeHomeMoveExecutable, "--no-copy", "--update=none-fail", "--no-target-directory", "--", "/proc/self/fd/3/"+sourceName, "/proc/self/fd/4/"+destinationName)
	cmd.ExtraFiles = []*os.File{source, destination}
	cmd.Env = []string{"LC_ALL=C"}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("no-copy/no-overwrite native home move: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
