package readiness

type Phase string

const (
	PhaseRuntime Phase = "runtime"
	PhaseRepo    Phase = "repo"
)

type Check struct {
	Name                string `json:"name"`
	Phase               Phase  `json:"phase"`
	OK                  bool   `json:"ok"`
	RequiredForCapacity bool   `json:"required_for_capacity"`
	Retryable           bool   `json:"retryable"`
	Detail              string `json:"detail,omitempty"`
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

func (r *Report) AddRuntime(name string, ok bool, detail string) {
	r.Checks = append(r.Checks, Check{
		Name:                name,
		Phase:               PhaseRuntime,
		OK:                  ok,
		RequiredForCapacity: true,
		Retryable:           true,
		Detail:              detail,
	})
}

func (r *Report) AddRepo(name string, ok bool, detail string) {
	r.Checks = append(r.Checks, Check{
		Name:                name,
		Phase:               PhaseRepo,
		OK:                  ok,
		RequiredForCapacity: false,
		Retryable:           true,
		Detail:              detail,
	})
}
