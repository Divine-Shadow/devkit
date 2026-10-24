package nativecmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFreshSelectedPreparationRejectsConcurrentSubstitutionBeforeEitherPhase(t *testing.T) {
	for _, phase := range []string{"bootstrap", "launch"} {
		t.Run(phase, func(t *testing.T) {
			p, opts := freshSelectedFixture(t)
			claim, err := claimFreshSelectedPreparation(p, opts, false)
			if err != nil {
				t.Fatal(err)
			}
			defer claim.close()
			if phase == "launch" {
				if err := claim.runPhase(func() error {
					if err := os.MkdirAll(p.Agent.HostWorktree, 0700); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(p.Agent.HostWorktree, "accepted-source"), []byte("new"), 0600)
				}); err != nil {
					t.Fatal(err)
				}
			}
			root := claim.directories[0].Name()
			completed := make(chan error, 1)
			go func() {
				if err := os.Rename(root, root+".retained"); err != nil {
					completed <- err
					return
				}
				if err := os.Mkdir(root, 0700); err != nil {
					completed <- err
					return
				}
				completed <- os.WriteFile(filepath.Join(root, "foreign"), []byte("untouched"), 0600)
			}()
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			called := false
			err = claim.runPhase(func() error {
				called = true
				return os.WriteFile(filepath.Join(root, "wrong-effect"), []byte("bad"), 0600)
			})
			if err == nil || called {
				t.Fatal("effect entered a substituted namespace")
			}
			b, err := os.ReadFile(filepath.Join(root, "foreign"))
			if err != nil || string(b) != "untouched" {
				t.Fatal("foreign replacement changed")
			}
			if _, err := os.Lstat(filepath.Join(root, "wrong-effect")); !os.IsNotExist(err) {
				t.Fatal("wrote into replacement")
			}
			if err := claim.complete(p); err == nil {
				t.Fatal("substituted namespace completed")
			}
		})
	}
}

func TestFreshSelectedPreparationHoldsExistingLifecycleCoordination(t *testing.T) {
	p, opts := freshSelectedFixture(t)
	claim, err := claimFreshSelectedPreparation(p, opts, false)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.close()
	coordination, err := nativeSlotResetCoordinationRoot(opts.WorktreeRoot)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		lock, err := acquireNativeSlotResetLock(coordination, p.Agent.ID.Project)
		if err == nil {
			_ = lock.release()
		}
		result <- err
	}()
	if err := <-result; err == nil {
		t.Fatal("competing supported reset acquired ownership")
	}
}
