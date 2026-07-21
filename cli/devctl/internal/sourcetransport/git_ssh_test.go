package sourcetransport

import "testing"

func TestValidateGitSSHArgs(t *testing.T) {
	valid := [][]string{
		{"git@github.com", "git-upload-pack 'Divine-Shadow/devkit.git'"},
		{"-o", "SendEnv=GIT_PROTOCOL", "git@ssh.github.com", "git-upload-pack '/Divine-Shadow/devkit.git'"},
		{"-p", "443", "git@ssh.github.com", "git-upload-pack 'Divine-Shadow/devkit.git'"},
		{"-G", "-o", "SendEnv=GIT_PROTOCOL", "git@github.com"},
	}
	for _, args := range valid {
		if err := ValidateGitSSHArgs(args); err != nil {
			t.Fatalf("valid argv %q rejected: %v", args, err)
		}
	}
	invalid := [][]string{
		nil,
		{"example.com", "git-upload-pack 'x/y.git'"},
		{"root@github.com", "git-upload-pack 'x/y.git'"},
		{"-o", "ProxyCommand=sh", "git@github.com", "git-upload-pack 'x/y.git'"},
		{"-p", "22", "git@github.com", "git-upload-pack 'x/y.git'"},
		{"-G", "git@github.com", "git-upload-pack 'x/y.git'"},
		{"git@github.com"},
		{"git@github.com", "git-receive-pack 'x/y.git'"},
		{"git@github.com", "git-upload-pack '../../x.git'"},
		{"git@github.com", "git-upload-pack 'x/y.git;sh'"},
	}
	for _, args := range invalid {
		if err := ValidateGitSSHArgs(args); err == nil {
			t.Fatalf("invalid argv %q accepted", args)
		}
	}
}

func TestOpenSSHEnvironmentBindsOnlyPackageShellAndGitProtocol(t *testing.T) {
	const shell = "/nix/store/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bash/bin/bash"

	environment, err := openSSHEnvironment(shell, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 1 || environment[0] != "SHELL="+shell {
		t.Fatalf("unexpected OpenSSH environment without protocol: %#v", environment)
	}

	environment, err = openSSHEnvironment(shell, "version=2")
	if err != nil {
		t.Fatal(err)
	}
	if len(environment) != 2 || environment[0] != "SHELL="+shell || environment[1] != "GIT_PROTOCOL=version=2" {
		t.Fatalf("unexpected OpenSSH environment with protocol: %#v", environment)
	}

	if _, err := openSSHEnvironment(shell, "version=1"); err == nil {
		t.Fatal("unsupported Git protocol was accepted")
	}
}
