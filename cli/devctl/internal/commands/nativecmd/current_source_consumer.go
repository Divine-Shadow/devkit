package nativecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

// The authoritative composition selects the actual consumer package and target
// together. Neither a runtime flag nor an inherited environment value selects
// a compatible helper. Empty bindings serve only this executable's own package.
var currentSourceConsumerRuntimeRoot, currentSourceConsumerTargetID string

func currentSourceConsumerHelper(authority string, consumer nativeplan.GUIConsumerWorkspace) (string, error) {
	return resolveCurrentSourceConsumerHelperFrom(authority, currentSourceConsumerRuntimeRoot, currentSourceConsumerTargetID, consumer, "/nix/store")
}

func resolveCurrentSourceConsumerHelperFrom(authority, boundRuntime, boundTarget string, consumer nativeplan.GUIConsumerWorkspace, storeRoot string) (string, error) {
	runtime := authority
	if boundRuntime != "" || boundTarget != "" {
		if boundRuntime == "" || boundTarget == "" || boundTarget != consumer.TargetID {
			return "", fmt.Errorf("source preparation requires its source-selected consumer and target binding")
		}
		runtime = boundRuntime
	}
	if !filepath.IsAbs(runtime) || filepath.Clean(runtime) != runtime || !strings.HasPrefix(runtime, storeRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("source preparation requires an immutable selected consumer runtime")
	}
	helper := filepath.Join(runtime, "kit", "bin", "devctl")
	actual, err := filepath.EvalSymlinks(helper)
	if err != nil || actual != helper {
		return "", fmt.Errorf("source preparation requires the selected consumer's canonical Devctl helper")
	}
	info, err := os.Stat(helper)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("source preparation requires the selected consumer's executable Devctl helper")
	}
	return helper, nil
}
