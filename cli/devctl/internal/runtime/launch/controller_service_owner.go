package launch

import (
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// Compiled production owner, not caller selectable. The variable allows a
// hermetic Unix socket fixture without impersonating a live privileged actor.
var controllerServiceOwnerUID = 0
var controllerServiceOwnerGroup = "users"

func validateControllerServiceOwnerClientPlan(p nativeplan.Plan) error {
	count := 0
	for _, bind := range p.Binds {
		if bind.Target != nativeplan.ControllerServiceOwnerSocket && bind.Target != nativeplan.ControllerServiceOwnerConfig {
			continue
		}
		count++
		if bind.Source != bind.Target || bind.Mode != "ro" || !bind.Required {
			return fmt.Errorf("controller service owner client projection is not exact and read-only")
		}
	}
	if count == 0 {
		return nil
	}
	if count != 2 || !nativeplan.IsControllerServiceOwnerClient(p.Agent.ID.Project, p.Agent.ID.Index, p.Agent.ID.Repo, p.ControllerProfile) {
		return fmt.Errorf("controller service owner client projection is not the registered Runtime lane")
	}
	if !planHasExactBind(p.Binds, nativeplan.Bind{Source: nativeplan.ControllerServiceOwnerSocket, Target: nativeplan.ControllerServiceOwnerSocket, Mode: "ro", Required: true}) ||
		!planHasExactBind(p.Binds, nativeplan.Bind{Source: nativeplan.ControllerServiceOwnerConfig, Target: nativeplan.ControllerServiceOwnerConfig, Mode: "ro", Required: true}) {
		return fmt.Errorf("controller service owner client projection lacks its fixed socket/role pair")
	}
	if _, err := nativeplan.ResolveControllerServiceOwnerConfig(); err != nil {
		return err
	}
	group, err := user.LookupGroup(controllerServiceOwnerGroup)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		path   string
		mode   os.FileMode
		socket bool
	}{
		{nativeplan.ControllerServiceOwnerSocket, 0770, true},
		{filepath.Dir(nativeplan.ControllerServiceOwnerSocket), 0750, false},
	} {
		info, err := os.Lstat(entry.path)
		if err != nil {
			return fmt.Errorf("controller service owner capability unavailable: %w", err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != controllerServiceOwnerUID || int(stat.Gid) != gid || info.Mode().Perm() != entry.mode || info.Mode()&os.ModeSymlink != 0 ||
			(entry.socket && info.Mode()&os.ModeSocket == 0) || (!entry.socket && !info.IsDir()) {
			return fmt.Errorf("controller service owner socket/parent is not the fixed existing owner/group and protected mode")
		}
	}
	return nil
}
