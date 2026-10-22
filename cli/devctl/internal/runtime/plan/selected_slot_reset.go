package plan

import (
	"fmt"
	"path/filepath"
	"reflect"
)

// BuildSelectedSlotReset constructs one GUI-owned Product execution plan.
// Shared lifecycle manifests retain their prefix geometry through BuildDevAll.
// A caller workspace is never an input: the immutable GUI projection first
// selects the host lane, then the ordinary isolated-plan validator projects it.
func BuildSelectedSlotReset(opts BuildOptions) (Plan, error) {
	if opts.Project != "dev-all" || opts.Repo != "ouroboros-ide" || opts.Index < 1 || opts.WorkspaceRoot != "" || (opts.GUITargetID == "" && !opts.RequireGUIConfig) {
		return Plan{}, fmt.Errorf("selected-slot reset requires a source-selected Product lane without a caller workspace")
	}
	source, err := BuildDevAll(opts)
	if err != nil {
		return Plan{}, err
	}
	projection := source.GUITargetConfig
	if projection == nil {
		return Plan{}, fmt.Errorf("selected-slot reset requires an immutable GUI projection")
	}
	if projection.Kind != "devkit-agent" && projection.Kind != "local-wsl-devkit-agent" {
		return source, nil
	}
	opts.WorkspaceRoot = filepath.Dir(source.Agent.HostWorktree)
	// Reuse exactly the first selection even when admission used unique geometry.
	opts.GUITargetID = projection.TargetID
	opts.RequireGUIConfig = false
	isolated, err := BuildDevAll(opts)
	if err != nil {
		return Plan{}, err
	}
	if !reflect.DeepEqual(isolated.GUITargetConfig, projection) {
		return Plan{}, fmt.Errorf("selected-slot reset GUI projection changed while constructing isolated geometry")
	}
	return isolated, nil
}
