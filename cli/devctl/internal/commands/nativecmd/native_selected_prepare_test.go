package nativecmd

import (
	"devkit/cli/devctl/internal/config"
	wtx "devkit/cli/devctl/internal/worktrees"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/devkitpaths"
)

func TestDevAllSelectedPrepareRejectsOverridesBeforeEffects(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	for _, flag := range []string{
		"--count", "--workspace-root", "--worktree-root", "--state-root",
		"--host-root", "--worktree-container-root", "--state-container-root",
		"--gui-target-id",
	} {
		t.Run(flag, func(t *testing.T) {
			value := "/caller/override"
			if flag == "--count" {
				value = "1"
			}
			ctx := &cmdregistry.Context{
				Project: "dev-all",
				Paths:   devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}},
				Args:    []string{"prepare", "--repo", "ouroboros-ide", "--index", "1", flag, value},
			}
			err := handlePrepare(ctx)
			if err == nil || (!strings.Contains(err.Error(), "derives roots and capacity") &&
				!strings.Contains(err.Error(), "without caller target or capacity") &&
				!(flag == "--host-root" && strings.Contains(err.Error(), "unknown native arg --host-root"))) {
				t.Fatalf("%s did not refuse at selected preparation admission: %v", flag, err)
			}
		})
	}
}

func TestDevAllSelectedPrepareSourceGeometry(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	ctx := &cmdregistry.Context{Project: "dev-all", Paths: devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}}}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			ctx.Args = []string{"prepare", "--repo", "ouroboros-ide", "--index", fmt.Sprint(index)}
			parsed, err := parsePlanArgs(ctx, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if err = applyNativeConfigDefaults(ctx, cfg, &parsed.opts); err != nil {
				t.Fatal(err)
			}
			p, acquisition, err := planSelectedDevAllPrepare(ctx, cfg, parsed)
			if err != nil {
				t.Fatal(err)
			}
			host := fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d", index)
			sandbox := "/workspaces/dev"
			home := fmt.Sprintf(".devhome-agent%d", index)
			if index == 1 {
				host += "/ouroboros-ide"
				sandbox += "/ouroboros-ide"
			}
			want := []string{fmt.Sprintf("/home/bayesartre/dev/agent-worktrees/agent%d/ouroboros-ide", index), filepath.Join(host, home), fmt.Sprintf("/home/bayesartre/dev/.devkit/native-agents/dev-all-agent%d", index), "/workspaces/dev/ouroboros-ide", filepath.Join(sandbox, home)}
			got := []string{p.Agent.HostWorktree, p.Agent.HostHome, p.Agent.StateRoot, p.Agent.SandboxWorktree, p.Agent.SandboxHome}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("selected geometry=%q want %q", got, want)
			}
			if p.Agent.ID.Index != index || acquisition.Index != index || acquisition.Count != 3 || acquisition.WorktreeRoot != "/home/bayesartre/dev/agent-worktrees" || acquisition.Origin != "ssh://git@ssh.github.com:443/Divine-Shadow/ouroboros-ide.git" || !acquisition.RequireSSHOrigin || acquisition.ReconstructSelected {
				t.Fatalf("wrong selected acquisition: %+v, agent=%+v", acquisition, p.Agent)
			}
		})
	}
}

func TestDevAllSelectedPrepareRejectsInvalidIndex(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	for _, index := range []string{"0", "-1", "4", "not-an-index"} {
		t.Run(index, func(t *testing.T) {
			ctx := &cmdregistry.Context{Project: "dev-all", Paths: devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}}, Args: []string{"prepare", "--index", index}}
			if err := handlePrepare(ctx); err == nil {
				t.Fatal("invalid selection admitted")
			}
		})
	}
}

// Use real Git acquisition against a local test remote. The runtime plan and
// acquisition options are the same pair consumed by selected preparation;
// no retained host lane, credential or runtime service is exercised here.
func TestDevAllSelectedPrepareAcquiresOnlyFreshLane(t *testing.T) {
	root := devkitSourceRootForNativeTest(t)
	fixture := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		b, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, b)
		}
		return strings.TrimSpace(string(b))
	}
	seed := filepath.Join(fixture, "seed")
	origin := filepath.Join(fixture, "origin.git")
	runGit("init", "--bare", origin)
	runGit("init", seed)
	if err := os.WriteFile(filepath.Join(seed, "payload"), []byte("source-derived fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runGit("-C", seed, "add", "payload")
	runGit("-C", seed, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
	runGit("-C", seed, "branch", "-M", "main")
	runGit("-C", seed, "push", origin, "main")
	ctx := &cmdregistry.Context{Project: "dev-all", Paths: devkitpaths.Paths{Root: root, OverlayPaths: []string{filepath.Join(root, "overlays")}}}
	cfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.Repo = "ouroboros-ide"
	cfg.Defaults.Origin = origin
	cfg.Native.HostRoot = fixture
	cfg.Native.WorktreeRoot = filepath.Join(fixture, "lanes")
	cfg.Native.StateRoot = filepath.Join(fixture, "state")
	selected := func(index int) (wtx.NativeOptions, string) {
		t.Helper()
		ctx.Args = []string{"prepare", "--index", fmt.Sprint(index)}
		parsed, err := parsePlanArgs(ctx, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyNativeConfigDefaults(ctx, cfg, &parsed.opts); err != nil {
			t.Fatal(err)
		}
		p, opts, err := planSelectedDevAllPrepare(ctx, cfg, parsed)
		if err != nil {
			t.Fatal(err)
		}
		// Production retains required package SSH. This fixture substitutes only
		// its local Git transport after planning; no credential or live lane is used.
		if !opts.RequireSSHOrigin {
			t.Fatal("Product planner dropped SSH authority")
		}
		opts.RequireSSHOrigin = false
		return opts, p.Agent.HostWorktree
	}
	// Retain lanes 1 and 3, including a dirty worktree, Git locks and opaque home/state bytes.
	retained := []string{}
	for _, index := range []int{1, 3} {
		opts, wt := selected(index)
		if err := wtx.SetupNative(opts); err != nil {
			t.Fatal(err)
		}
		common := filepath.Join(cfg.Native.WorktreeRoot, ".devkit", "git", fmt.Sprintf("agent%d", index), cfg.Defaults.Repo+".git")
		home := filepath.Join(filepath.Dir(wt), fmt.Sprintf(".devhome-agent%d", index))
		if index == 1 {
			home = filepath.Join(wt, ".devhome-agent1")
		}
		state := filepath.Join(cfg.Native.StateRoot, fmt.Sprintf("dev-all-agent%d", index))
		for _, dir := range []string{home, state} {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "retained"), []byte{0, 1, 2, 255}, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, file := range []string{filepath.Join(wt, "dirty"), filepath.Join(common, "index.lock")} {
			if err := os.WriteFile(file, []byte("retained custody\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		retained = append(retained, wt, common, home, state)
	}
	manifest := filepath.Join(cfg.Native.StateRoot, "manifest.json")
	if err := os.WriteFile(manifest, []byte("shared manifest must not be rewritten\n"), 0600); err != nil {
		t.Fatal(err)
	}
	retained = append(retained, manifest)
	snapshot := func() string {
		t.Helper()
		var out strings.Builder
		for _, r := range retained {
			err := filepath.WalkDir(r, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, err := d.Info()
				if err != nil {
					return err
				}
				fmt.Fprintf(&out, "%s:%v:%d:%d:", path, info.Mode(), info.ModTime().UnixNano(), info.Size())
				if info.Mode().IsRegular() {
					b, e := os.ReadFile(path)
					if e != nil {
						return e
					}
					fmt.Fprintf(&out, "%x", b)
				}
				if info.Mode()&os.ModeSymlink != 0 {
					link, e := os.Readlink(path)
					if e != nil {
						return e
					}
					fmt.Fprint(&out, link)
				}
				out.WriteByte('\n')
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return out.String()
	}
	before := snapshot()
	opts, wt := selected(2)
	if err := wtx.SetupNative(opts); err != nil {
		t.Fatal(err)
	}
	if got := runGit("-C", wt, "rev-parse", "HEAD"); got != runGit("-C", seed, "rev-parse", "HEAD") {
		t.Fatal("fresh selected checkout has wrong source identity")
	}
	if got := runGit("-C", wt, "status", "--porcelain"); got != "" {
		t.Fatalf("fresh checkout dirty: %s", got)
	}
	if got := runGit("-C", wt, "symbolic-ref", "--short", "HEAD"); got != "agent2" {
		t.Fatalf("wrong branch: %s", got)
	}
	if after := snapshot(); after != before {
		t.Fatal("selected acquisition changed retained worktree/Git/home/state/manifest bytes or metadata")
	}
	if _, err := os.Stat(filepath.Join(cfg.Native.WorktreeRoot, "agent4")); !os.IsNotExist(err) {
		t.Fatalf("undeclared lane created: %v", err)
	}
}
