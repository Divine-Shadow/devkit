package runtimematrix

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
)

// Entry is one overlay's declared runtime pairing.
type Entry struct {
	Overlay             string
	Repo                string
	Service             string
	Image               string
	Flake               string
	FlakeInputOverrides map[string]string
	CodexVersion        string
	CoreCheck           string
	Canonical           bool
	ConfigPath          string
	FlakePath           string
	RuntimeWorkDir      string
}

// Register adds runtime-matrix to the command registry.
func Register(r *cmdregistry.Registry) {
	r.Register("runtime-matrix", handle)
}

func handle(ctx *cmdregistry.Context) error {
	includeAll := false
	check := false
	var overlays []string
	var repos []string
	for i := 0; i < len(ctx.Args); i++ {
		arg := ctx.Args[i]
		switch arg {
		case "--all":
			includeAll = true
		case "--check":
			check = true
		case "--overlay":
			value, err := nextRuntimeMatrixArg(ctx.Args, &i, "--overlay")
			if err != nil {
				return err
			}
			overlays = append(overlays, value)
		case "--repo":
			value, err := nextRuntimeMatrixArg(ctx.Args, &i, "--repo")
			if err != nil {
				return err
			}
			repos = append(repos, value)
		default:
			if value, ok := strings.CutPrefix(arg, "--overlay="); ok {
				overlays = append(overlays, strings.TrimSpace(value))
				continue
			}
			if value, ok := strings.CutPrefix(arg, "--repo="); ok {
				repos = append(repos, strings.TrimSpace(value))
				continue
			}
			return fmt.Errorf("unknown runtime-matrix argument %q", arg)
		}
	}
	entries, err := Discover(ctx.Paths.OverlayPaths, includeAll)
	if err != nil {
		return err
	}
	entries, err = Filter(entries, repos, overlays)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no runtime pairings declared")
	}
	Print(entries)
	if check {
		return Check(entries, ctx.DryRun)
	}
	return nil
}

func nextRuntimeMatrixArg(args []string, index *int, flag string) (string, error) {
	if *index+1 >= len(args) {
		return "", fmt.Errorf("%s requires a value", flag)
	}
	*index = *index + 1
	value := strings.TrimSpace(args[*index])
	if value == "" {
		return "", fmt.Errorf("%s requires a non-empty value", flag)
	}
	return value, nil
}

// Discover reads overlay devkit.yaml files and returns runtime entries.
func Discover(overlayRoots []string, includeAll bool) ([]Entry, error) {
	var entries []Entry
	for _, root := range overlayRoots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			if strings.HasPrefix(dir.Name(), "_") && !includeAll {
				continue
			}
			overlay := dir.Name()
			cfg, cfgDir, err := config.ReadAll([]string{root}, overlay)
			if err != nil {
				return nil, err
			}
			if cfgDir == "" {
				continue
			}
			image := strings.TrimSpace(cfg.Runtime.Image)
			flake := strings.TrimSpace(cfg.Runtime.Flake)
			if image == "" && flake == "" && !includeAll {
				continue
			}
			canonical := true
			if cfg.Runtime.Canonical != nil {
				canonical = *cfg.Runtime.Canonical
			}
			if !canonical && !includeAll {
				continue
			}
			repo := strings.TrimSpace(cfg.Defaults.Repo)
			service := strings.TrimSpace(cfg.Service)
			if service == "" {
				service = "dev-agent"
			}
			entries = append(entries, Entry{
				Overlay: overlay,
				Repo:    repo,
				Service: service,
				Image:   image,
				Flake:   flake,
				FlakeInputOverrides: config.ResolveRuntimeFlakeInputOverrides(
					config.RuntimeFlakeInputOverrideRoot(filepath.Dir(root), cfg.Native.HostRoot),
					cfg.Runtime.FlakeInputOverrides,
				),
				CodexVersion:   strings.TrimSpace(cfg.Runtime.CodexVersion),
				CoreCheck:      strings.TrimSpace(cfg.Runtime.CoreCheck),
				Canonical:      canonical,
				ConfigPath:     filepath.Join(cfgDir, "devkit.yaml"),
				FlakePath:      filepath.Join(cfgDir, "flake.nix"),
				RuntimeWorkDir: filepath.Dir(root),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repo == entries[j].Repo {
			return entries[i].Overlay < entries[j].Overlay
		}
		return entries[i].Repo < entries[j].Repo
	})
	return entries, nil
}

// Filter selects discovered entries by repo or overlay.
func Filter(entries []Entry, repos, overlays []string) ([]Entry, error) {
	repoSet := stringSet(repos)
	overlaySet := stringSet(overlays)
	if len(repoSet) == 0 && len(overlaySet) == 0 {
		return entries, nil
	}
	var filtered []Entry
	for _, entry := range entries {
		if repoSet[entry.Repo] || overlaySet[entry.Overlay] {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no runtime pairings matched repo %s or overlay %s", selectorSummary(repoSet), selectorSummary(overlaySet))
	}
	return filtered, nil
}

func stringSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func selectorSummary(values map[string]bool) string {
	if len(values) == 0 {
		return "[]"
	}
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return "[" + strings.Join(keys, ",") + "]"
}

// Print writes a compact operator-facing matrix to stdout.
func Print(entries []Entry) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tOVERLAY\tSERVICE\tRUNTIME\tCODEX\tCORE CHECK")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Repo, e.Overlay, e.Service, runtimeArtifact(e), e.CodexVersion, e.CoreCheck)
	}
	_ = w.Flush()
}

// Check verifies the matrix is internally consistent and runtimes carry Codex.
func Check(entries []Entry, dryRun bool) error {
	var problems []string
	seenRepo := map[string]string{}
	for _, e := range entries {
		if e.Repo == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime repo/defaults.repo is empty", e.Overlay))
		}
		if e.Flake == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.flake is empty", e.Overlay))
		}
		if e.Image != "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.image is retired metadata; declare runtime.flake instead", e.Overlay))
		}
		if e.Flake != "" {
			if !flakeRefAccepted(e.Overlay, e.Flake) {
				problems = append(problems, fmt.Sprintf("%s: runtime.flake %s is not an accepted ref (%s)", e.Overlay, e.Flake, strings.Join(acceptedFlakeRefs(e.Overlay), " or ")))
			}
			if strings.TrimSpace(e.FlakePath) == "" {
				problems = append(problems, fmt.Sprintf("%s: overlay-local flake path is empty", e.Overlay))
			} else if _, err := os.Stat(e.FlakePath); err != nil {
				if os.IsNotExist(err) {
					problems = append(problems, fmt.Sprintf("%s: missing overlay-local flake.nix", e.Overlay))
				} else {
					problems = append(problems, fmt.Sprintf("%s: stat overlay-local flake: %v", e.Overlay, err))
				}
			}
			for name, ref := range e.FlakeInputOverrides {
				if path, ok := strings.CutPrefix(ref, "path:"); ok {
					if _, err := os.Stat(path); err != nil {
						if os.IsNotExist(err) {
							problems = append(problems, fmt.Sprintf("%s: flake input override %s path missing: %s", e.Overlay, name, path))
						} else {
							problems = append(problems, fmt.Sprintf("%s: stat flake input override %s: %v", e.Overlay, name, err))
						}
					}
				}
			}
		}
		if e.CodexVersion == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.codex_version is empty", e.Overlay))
		}
		if e.CoreCheck == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.core_check is empty", e.Overlay))
		}
		if prev, ok := seenRepo[e.Repo]; ok && e.Canonical {
			problems = append(problems, fmt.Sprintf("%s: repo %s is already paired by overlay %s", e.Overlay, e.Repo, prev))
		} else if e.Repo != "" && e.Canonical {
			seenRepo[e.Repo] = e.Overlay
		}
		if e.Flake != "" && e.CodexVersion != "" {
			if dryRun {
				fmt.Printf("[dry-run] %s\n", strings.Join(flakeCodexVersionArgs(e.Flake, e.FlakeInputOverrides), " "))
			} else if got, err := flakeCodexVersion(e.Flake, e.FlakeInputOverrides, e.RuntimeWorkDir); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %s codex version check failed: %v", e.Overlay, e.Flake, err))
			} else if got != e.CodexVersion {
				problems = append(problems, fmt.Sprintf("%s: %s codex version %s, want %s", e.Overlay, e.Flake, got, e.CodexVersion))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("runtime matrix check failed:\n- %s", strings.Join(problems, "\n- "))
	}
	fmt.Println("runtime-matrix: OK")
	return nil
}

func runtimeArtifact(e Entry) string {
	if e.Flake != "" {
		return e.Flake
	}
	return e.Image
}

func acceptedFlakeRefs(overlay string) []string {
	return []string{overlayRootedFlakeRef(overlay), overlayLocalFlakeRef(overlay)}
}

func flakeRefAccepted(overlay string, flake string) bool {
	for _, accepted := range acceptedFlakeRefs(overlay) {
		if flake == accepted {
			return true
		}
	}
	return false
}

func overlayLocalFlakeRef(overlay string) string {
	return "./overlays/" + overlay + "#default"
}

func overlayRootedFlakeRef(overlay string) string {
	return "path:.?dir=overlays/" + overlay + "#default"
}

func flakeCodexVersion(flake string, overrides map[string]string, workDir string) (string, error) {
	args := flakeCodexVersionArgs(flake, overrides)
	cmd := exec.Command(args[0], args[1:]...)
	if strings.TrimSpace(workDir) != "" {
		cmd.Dir = workDir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty codex version output")
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v"), nil
}

func flakeCodexVersionArgs(flake string, overrides map[string]string) []string {
	args := []string{"nix", "--extra-experimental-features", "nix-command flakes", "develop"}
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--override-input", name, overrides[name])
	}
	args = append(args, flake, "--output-lock-file", "/dev/null", "--command", "bash", "-c", "codex --version")
	return args
}
