package sqliteauthority

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageRequiresAbsoluteExecutablePackageBinding(t *testing.T) {
	for _, test := range []struct {
		name       string
		executable string
		want       string
	}{
		{name: "missing", want: "not bound"},
		{name: "relative", executable: "sqlite3", want: "must be absolute"},
		{name: "absent", executable: filepath.Join(t.TempDir(), "sqlite3"), want: "validate package-owned"},
	} {
		t.Run(test.name, func(t *testing.T) {
			restore := SetExecutableForTesting(test.executable)
			defer restore()
			_, err := Package()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Package error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestPackageReturnsExactExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqlite3")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	restore := SetExecutableForTesting(path)
	defer restore()
	got, err := Package()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("Package = %q, want %q", got, path)
	}
}
