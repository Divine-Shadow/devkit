package imagematrix

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
	Overlay      string
	Repo         string
	Service      string
	Image        string
	Flake        string
	CodexVersion string
	CoreCheck    string
	Canonical    bool
	ConfigPath   string
	FlakePath    string
}

// Register adds image-matrix to the command registry.
func Register(r *cmdregistry.Registry) {
	r.Register("image-matrix", handle)
}

func handle(ctx *cmdregistry.Context) error {
	includeAll := false
	check := false
	for _, arg := range ctx.Args {
		switch arg {
		case "--all":
			includeAll = true
		case "--check":
			check = true
		default:
			return fmt.Errorf("unknown image-matrix argument %q", arg)
		}
	}
	entries, err := Discover(ctx.Paths.OverlayPaths, includeAll)
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
				Overlay:      overlay,
				Repo:         repo,
				Service:      service,
				Image:        image,
				Flake:        flake,
				CodexVersion: strings.TrimSpace(cfg.Runtime.CodexVersion),
				CoreCheck:    strings.TrimSpace(cfg.Runtime.CoreCheck),
				Canonical:    canonical,
				ConfigPath:   filepath.Join(cfgDir, "devkit.yaml"),
				FlakePath:    filepath.Join(cfgDir, "flake.nix"),
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
			problems = append(problems, fmt.Sprintf("%s: runtime.image is legacy; declare runtime.flake instead", e.Overlay))
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
				fmt.Printf("[dry-run] nix --extra-experimental-features 'nix-command flakes' develop %s --no-write-lock-file --command bash -lc 'codex --version'\n", e.Flake)
			} else if got, err := flakeCodexVersion(e.Flake); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %s codex version check failed: %v", e.Overlay, e.Flake, err))
			} else if got != e.CodexVersion {
				problems = append(problems, fmt.Sprintf("%s: %s codex version %s, want %s", e.Overlay, e.Flake, got, e.CodexVersion))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("image matrix check failed:\n- %s", strings.Join(problems, "\n- "))
	}
	fmt.Println("image-matrix: OK")
	return nil
}

func runtimeArtifact(e Entry) string {
	if e.Flake != "" {
		return e.Flake
	}
	return e.Image
}

func acceptedFlakeRefs(overlay string) []string {
	return []string{overlayLocalFlakeRef(overlay)}
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

func flakeCodexVersion(flake string) (string, error) {
	cmd := exec.Command("nix", "--extra-experimental-features", "nix-command flakes", "develop", flake, "--no-write-lock-file", "--command", "bash", "-lc", "codex --version")
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
