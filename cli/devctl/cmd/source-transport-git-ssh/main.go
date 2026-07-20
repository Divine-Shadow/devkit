package main

import (
	"fmt"
	"os"

	"devkit/cli/devctl/internal/sourcetransport"
)

func main() {
	if err := sourcetransport.RunGitSSH(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "devkit-source-git-ssh:", err)
		os.Exit(2)
	}
}
