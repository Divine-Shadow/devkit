//go:build devkitintegration

package plan

import (
	"os"
	"strings"
)

func init() {
	if source := strings.TrimSpace(os.Getenv("DEVKIT_INTEGRATION_NSCD_SOURCE")); source != "" {
		WorkspaceEgressNSCDSource = source
	}
}
