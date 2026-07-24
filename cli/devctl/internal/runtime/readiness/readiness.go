package readiness

type Phase string

const (
	PhaseRuntime Phase = "runtime"
	PhaseRepo    Phase = "repo"
)

type Check struct {
	Name                string `json:"name"`
	Phase               Phase  `json:"phase"`
	Component           string `json:"component"`
	OK                  bool   `json:"ok"`
	RequiredForCapacity bool   `json:"required_for_capacity"`
	Retryable           bool   `json:"retryable"`
	Detail              string `json:"detail,omitempty"`
	Action              string `json:"action,omitempty"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func (r Report) RuntimeReady() bool {
	for _, check := range r.Checks {
		if check.Phase == PhaseRuntime && !check.OK {
			return false
		}
	}
	return true
}

func (r Report) RepoReady() bool {
	for _, check := range r.Checks {
		if check.Phase == PhaseRepo && !check.OK {
			return false
		}
	}
	return true
}

func (r Report) CapacityAvailable() bool {
	for _, check := range r.Checks {
		if check.RequiredForCapacity && !check.OK {
			return false
		}
	}
	return r.RuntimeReady()
}

func (r Report) HasRepoChecks() bool {
	for _, check := range r.Checks {
		if check.Phase == PhaseRepo {
			return true
		}
	}
	return false
}

func (r Report) FailedChecks() []Check {
	var failed []Check
	for _, check := range r.Checks {
		if !check.OK {
			failed = append(failed, check)
		}
	}
	return failed
}

func (r Report) Status() string {
	if !r.RuntimeReady() {
		return "blocked"
	}
	if !r.RepoReady() {
		return "degraded"
	}
	return "ready"
}

func (r Report) Action() string {
	for _, check := range r.Checks {
		if !check.OK && check.Action != "" {
			return check.Action
		}
	}
	return ""
}

func (r Report) ComponentState(component string) string {
	seen := false
	for _, check := range r.Checks {
		if check.Component != component {
			continue
		}
		seen = true
		if !check.OK {
			return "blocked"
		}
	}
	if !seen {
		return "unknown"
	}
	return "ready"
}

func (r *Report) AddRuntime(name string, ok bool, detail string) {
	r.Checks = append(r.Checks, Check{
		Name:                name,
		Phase:               PhaseRuntime,
		Component:           componentFor(PhaseRuntime, name),
		OK:                  ok,
		RequiredForCapacity: true,
		Retryable:           true,
		Detail:              detail,
		Action:              actionFor(PhaseRuntime, name, ok),
	})
}

func (r *Report) AddRepo(name string, ok bool, detail string) {
	r.Checks = append(r.Checks, Check{
		Name:                name,
		Phase:               PhaseRepo,
		Component:           componentFor(PhaseRepo, name),
		OK:                  ok,
		RequiredForCapacity: false,
		Retryable:           true,
		Detail:              detail,
		Action:              actionFor(PhaseRepo, name, ok),
	})
}

func componentFor(phase Phase, name string) string {
	if phase == PhaseRepo {
		return "repo"
	}
	switch name {
	case "prepare-state":
		return "worktree"
	case "broker-socket", "docker-client":
		return "broker"
	case "sandbox-command":
		return "sandbox"
	default:
		return "tooling"
	}
}

func actionFor(phase Phase, name string, ok bool) string {
	if ok {
		return ""
	}
	if phase == PhaseRepo {
		return "fix the repo check in the agent worktree, or rerun with --runtime-only when only runtime capacity is required"
	}
	switch name {
	case "prepare-state":
		return "prepare or repair the native worktree and agent state, then rerun up or ensure-ready"
	case "broker-socket", "docker-client":
		return "start or inspect the devkit broker and verify the configured broker socket"
	case "sandbox-command":
		return "inspect the native plan, Nix shell, bubblewrap binds, and agent state"
	default:
		return "fix the overlay runtime tool declaration or shell hook for this check"
	}
}
