package imagematrix

import (
	"bytes"
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
	ComposePath  string
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
			if !dir.IsDir() || strings.HasPrefix(dir.Name(), "_") {
				continue
			}
			overlay := dir.Name()
			cfg, cfgDir, err := config.ReadAll([]string{root}, overlay)
			if err != nil {
				return nil, err
			}
			image := strings.TrimSpace(cfg.Runtime.Image)
			flake := strings.TrimSpace(cfg.Runtime.Flake)
			if image == "" && flake == "" {
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
				ComposePath:  filepath.Join(cfgDir, "compose.override.yml"),
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
		if e.Image == "" && e.Flake == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.image or runtime.flake is empty", e.Overlay))
		}
		if e.CodexVersion == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.codex_version is empty", e.Overlay))
		}
		if e.CoreCheck == "" {
			problems = append(problems, fmt.Sprintf("%s: runtime.core_check is empty", e.Overlay))
		}
		if prev, ok := seenRepo[e.Repo]; ok {
			problems = append(problems, fmt.Sprintf("%s: repo %s is already paired by overlay %s", e.Overlay, e.Repo, prev))
		} else if e.Repo != "" {
			seenRepo[e.Repo] = e.Overlay
		}
		if e.Image != "" && e.ComposePath != "" {
			data, err := os.ReadFile(e.ComposePath)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: read compose override: %v", e.Overlay, err))
			} else if !bytes.Contains(data, []byte("image: "+e.Image)) {
				problems = append(problems, fmt.Sprintf("%s: compose override does not declare image: %s", e.Overlay, e.Image))
			}
		}
		if e.Image != "" && e.CodexVersion != "" {
			if dryRun {
				fmt.Printf("[dry-run] docker run --rm --entrypoint bash %s -lc '/usr/local/bin/codex --version'\n", e.Image)
			} else if got, err := imageCodexVersion(e.Image); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %s codex version check failed: %v", e.Overlay, e.Image, err))
			} else if got != e.CodexVersion {
				problems = append(problems, fmt.Sprintf("%s: %s codex version %s, want %s", e.Overlay, e.Image, got, e.CodexVersion))
			}
		}
		if e.Flake != "" && e.CodexVersion != "" {
			if dryRun {
				fmt.Printf("[dry-run] nix --extra-experimental-features 'nix-command flakes' develop %s --command bash -lc 'codex --version'\n", e.Flake)
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

func imageCodexVersion(image string) (string, error) {
	cmd := exec.Command("docker", "run", "--rm", "--entrypoint", "bash", image, "-lc", "/usr/local/bin/codex --version")
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

func flakeCodexVersion(flake string) (string, error) {
	cmd := exec.Command("nix", "--extra-experimental-features", "nix-command flakes", "develop", flake, "--command", "bash", "-lc", "codex --version")
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
