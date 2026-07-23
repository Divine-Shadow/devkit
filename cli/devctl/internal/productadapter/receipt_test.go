package productadapter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialHandleDigestsBindInboundGUIAuthorization(t *testing.T) {
	root := t.TempDir()
	consumer := ConsumerManifest{
		SSHIdentityPath:    filepath.Join(root, "id_ed25519"),
		SSHPublicKeyPath:   filepath.Join(root, "id_ed25519.pub"),
		AuthorizedKeysPath: filepath.Join(root, "authorized_keys"),
		CodexAuthPath:      filepath.Join(root, "auth.json"),
	}
	for path, payload := range map[string]string{
		consumer.SSHIdentityPath:    "private",
		consumer.SSHPublicKeyPath:   "ssh-ed25519 product\n",
		consumer.AuthorizedKeysPath: "restrict ssh-ed25519 gui\n",
		consumer.CodexAuthPath:      "{}\n",
	} {
		if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := CaptureCredentialHandleDigests(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 4 || before["authorized_keys"] == "" {
		t.Fatalf("credential receipt omitted GUI authorization: %#v", before)
	}
	if err := os.WriteFile(
		consumer.AuthorizedKeysPath,
		[]byte("restrict ssh-ed25519 replacement\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	after, err := CaptureCredentialHandleDigests(consumer)
	if err != nil {
		t.Fatal(err)
	}
	if before["authorized_keys"] == after["authorized_keys"] {
		t.Fatal("GUI authorization mutation did not invalidate its receipt digest")
	}
}
