package launch

import (
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Exercise actual preparation on the same non-secret Git/home fixture. The
// private preparation primitive uses temporary geometry; public TF1 admission
// is independently exercised by native command controls.
func TestTerraformToolsActualPreparationDefersOnlyProviders(t *testing.T) {
	// Model hostile ambient precedence with public owned data, never a real
	// inherited credential file. The nested fixture must select its own file.
	poison := filepath.Join(t.TempDir(), "ambient-credentials")
	writeTestFile(t, poison, "PUBLIC_POISON_FIXTURE\n")
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if _, err := syscall.InotifyAddWatch(fd, poison, syscall.IN_OPEN|syscall.IN_ACCESS|syscall.IN_MODIFY|syscall.IN_ATTRIB|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", poison)
	t.Run("explicit-owned-file", terraformToolsActualPreparationFixture)
	buffer := make([]byte, 8192)
	count, err := syscall.Read(fd, buffer)
	if count > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("synthetic ambient credential file was accessed or watch failed: %d %v", count, err)
	}
	if os.Getenv("AWS_SHARED_CREDENTIALS_FILE") != poison {
		t.Fatal("nested fixture did not restore its enclosing selector")
	}
}

func terraformToolsActualPreparationFixture(t *testing.T) {
	p := prepareNativeFixture(t, filepath.Join(t.TempDir(), "dev"), filepath.Join(t.TempDir(), "home"))
	p.Agent.ID.Project = "ouroboros-terraform"
	p.Agent.ID.Repo = "ouroboros-terraform"
	p.Agent.ID.Index = 1
	oldHydrator := terraformProviderCredentialHydrator
	oldRoot := terraformProviderCredentialRoot
	t.Cleanup(func() { terraformProviderCredentialHydrator = oldHydrator; terraformProviderCredentialRoot = oldRoot })
	marker := filepath.Join(t.TempDir(), "hydrator-called")
	terraformProviderCredentialHydrator = filepath.Join(t.TempDir(), "hydrator")
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, terraformProviderCredentialHydrator, "#!"+shell+"\necho called >> '"+marker+"'\necho 'Terraform provider credential hydration failed: approved DNSimple token source is unavailable' >&2\nexit 1\n")
	if err := os.Chmod(terraformProviderCredentialHydrator, 0700); err != nil {
		t.Fatal(err)
	}
	terraformProviderCredentialRoot = filepath.Join(t.TempDir(), "providers")
	aws := filepath.Join(t.TempDir(), "aws-source")
	target := filepath.Join(p.Agent.HostHome, ".aws")
	paths := []string{filepath.Join(aws, "credentials"), filepath.Join(target, "credentials"), filepath.Join(terraformProviderCredentialRoot, "google-adc.json"), filepath.Join(p.Agent.HostHome, ".config", "ouroboros", "dnsimple-provider.env")}
	for _, path := range paths {
		writeTestFile(t, path, "SYNTHETIC_PRESERVED_PROVIDER_FIXTURE\n")
	}
	t.Setenv("DEVKIT_AWS_HOME", aws)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(aws, "credentials"))
	if err := Prepare(p); err == nil || !strings.Contains(err.Error(), "approved DNSimple token source is unavailable") {
		t.Fatalf("normal preparation did not retain real missing-provider refusal: %v", err)
	}
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != "called\n" {
		t.Fatal("normal preparation did not invoke exactly one owned refusing hydrator")
	}
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	for _, path := range append(append([]string{}, paths...), aws, target, terraformProviderCredentialRoot) {
		if _, err := syscall.InotifyAddWatch(fd, path, syscall.IN_OPEN|syscall.IN_ACCESS|syscall.IN_MODIFY|syscall.IN_ATTRIB|syscall.IN_DELETE_SELF|syscall.IN_MOVE_SELF); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepare(p, false, true); err != nil {
		t.Fatalf("same-fixture tools preparation: %v", err)
	}
	buffer := make([]byte, 8192)
	count, err := syscall.Read(fd, buffer)
	if count > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("tools phase opened or changed provider material: %d %v", count, err)
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(after) != string(before) {
		t.Fatal("tools phase invoked provider hydrator")
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil || string(b) != "SYNTHETIC_PRESERVED_PROVIDER_FIXTURE\n" {
			t.Fatalf("preserved provider fixture changed %s: %v", path, err)
		}
	}
	required := p
	required.Binds = append(append([]nativeplan.Bind{}, p.Binds...), nativeplan.Bind{Source: filepath.Join(t.TempDir(), "missing-bind"), Required: true})
	if err := prepare(required, false, true); err == nil || !strings.Contains(err.Error(), "required bind source") {
		t.Fatalf("tools preparation bypassed required bind: %v", err)
	}
	configured := p
	configured.GUITargetConfig = &nativeplan.GUITargetConfigProjection{TargetID: "terraform-ouro-1", Source: filepath.Join(t.TempDir(), "missing-immutable-config"), SourceSHA256: strings.Repeat("0", 64)}
	if err := prepare(configured, false, true); err == nil {
		t.Fatal("tools preparation bypassed immutable Codex configuration")
	}
	p.Agent.HostWorktree = filepath.Join(t.TempDir(), "missing")
	if err := prepare(p, false, true); err == nil {
		t.Fatal("tools phase bypassed worktree custody")
	}
}

// Derive the missing bindings from the actual native TF1 planner, then run the
// actual preparation primitive in a non-secret temporary namespace. Only the
// unrelated external service binds are omitted from this fixture; their normal
// immutable checks and public target/host admission remain separate controls.
