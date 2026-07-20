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
