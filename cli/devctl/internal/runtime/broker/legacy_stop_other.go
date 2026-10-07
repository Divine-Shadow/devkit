//go:build !linux

package broker

func stopWitnessedLegacy(c Config, g *lifecycleGuard, status Status, dryRun bool) (bool, Status, error) {
	return false, status, nil
}
