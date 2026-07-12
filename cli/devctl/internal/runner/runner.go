package runner

import (
	"fmt"
	"os"
	"strings"
	"time"

	"devkit/cli/devctl/internal/execx"
)

// Host executes a host binary with a default 10 minute timeout.
func Host(dry bool, name string, args ...string) {
	ctx, cancel := execx.WithTimeout(10 * time.Minute)
	defer cancel()
	if dry {
		fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
		return
	}
	res := execx.RunCtx(ctx, name, args...)
	if res.Code != 0 {
		os.Exit(res.Code)
	}
}

// HostInteractive runs a host command without a timeout (for tmux attach, etc.).
func HostInteractive(dry bool, name string, args ...string) {
	if dry {
		fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
		return
	}
	res := execx.Run(name, args...)
	if res.Code != 0 {
		os.Exit(res.Code)
	}
}

// HostBestEffort executes a host command and ignores non-zero exits.
func HostBestEffort(dry bool, name string, args ...string) {
	ctx, cancel := execx.WithTimeout(2 * time.Minute)
	defer cancel()
	if dry {
		fmt.Fprintln(os.Stderr, "+ "+name+" "+strings.Join(args, " "))
		return
	}
	_ = execx.RunCtx(ctx, name, args...)
}
