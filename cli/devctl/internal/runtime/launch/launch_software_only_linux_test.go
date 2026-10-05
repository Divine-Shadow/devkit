package launch

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPrepareSoftwareOnlyNeverOpensOrChangesOwnedCredentialFixtures(t *testing.T) {
	storeRoot := withManagementSkillsStoreRoot(t)
	fixture := writeManagementSkillsFixture(t, storeRoot, "software-only-management-skills", strings.Repeat("a", 40), map[string]string{
		"fleet-health-hypervisor/SKILL.md": "owned public software fixture\n",
	})
	devRoot := filepath.Join(t.TempDir(), "dev")
	hostHome := filepath.Join(t.TempDir(), "existing-home")
	p := prepareManagementSkillsFixture(t, fixture, devRoot, hostHome)
	callerHome := filepath.Join(t.TempDir(), "owned-caller")
	authSource := filepath.Join(callerHome, "auth.json")
	authTarget := filepath.Join(hostHome, ".codex", "auth.json")
	awsSource := filepath.Join(callerHome, ".aws")
	awsTarget := filepath.Join(hostHome, ".aws")
	sshSource := filepath.Join(callerHome, ".ssh")
	sshTarget := filepath.Join(hostHome, ".ssh")
	for _, path := range []string{authSource, authTarget, filepath.Join(awsSource, "credentials"), filepath.Join(awsTarget, "credentials"), filepath.Join(sshSource, "id_ed25519"), filepath.Join(sshTarget, "id_ed25519")} {
		writeTestFile(t, path, "OWNED_SYNTHETIC_NO_ACCOUNT\n")
	}
	t.Setenv("HOME", callerHome)
	t.Setenv("CODEX_AUTH_JSON", authSource)
	t.Setenv("DEVKIT_AWS_HOME", awsSource)
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	for _, path := range []string{authSource, authTarget, awsSource, awsTarget, sshSource, sshTarget} {
		if _, err := syscall.InotifyAddWatch(fd, path, syscall.IN_OPEN|syscall.IN_ACCESS|syscall.IN_MODIFY|syscall.IN_ATTRIB|syscall.IN_CREATE|syscall.IN_DELETE|syscall.IN_MOVE); err != nil {
			t.Fatal(err)
		}
	}
	if err := PrepareSoftwareOnly(p); err != nil {
		t.Fatalf("software-only Prepare: %v", err)
	}
	buffer := make([]byte, 8192)
	n, err := syscall.Read(fd, buffer)
	if n > 0 || (err != nil && err != syscall.EAGAIN) {
		t.Fatalf("software-only preparation accessed or changed a credential fixture: events=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(hostHome, ".codex", "management-runtime-skills.json")); err != nil {
		t.Fatalf("public software did not converge: %v", err)
	}
}

func TestPrepareSoftwareOnlyRefusesFreshOrSymlinkedHome(t *testing.T) {
	storeRoot := withManagementSkillsStoreRoot(t)
	fixture := writeManagementSkillsFixture(t, storeRoot, "software-only-existing-home", strings.Repeat("a", 40), map[string]string{"fleet-health-hypervisor/SKILL.md": "public\n"})
	p := prepareManagementSkillsFixture(t, fixture, filepath.Join(t.TempDir(), "dev"), filepath.Join(t.TempDir(), "absent-home"))
	if err := PrepareSoftwareOnly(p); err == nil || !strings.Contains(err.Error(), "existing real native home") {
		t.Fatalf("fresh home accepted: %v", err)
	}
	actual := t.TempDir()
	if err := os.Symlink(actual, p.Agent.HostHome); err != nil {
		t.Fatal(err)
	}
	if err := PrepareSoftwareOnly(p); err == nil || !strings.Contains(err.Error(), "existing real native home") {
		t.Fatalf("symlink home accepted: %v", err)
	}
}
