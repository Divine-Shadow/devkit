package nativecmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	nativeagent "devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func TestSelectedStagingLifecycleDescriptorSubprocess(t *testing.T) {
	mode := os.Getenv("DEVKIT_TEST_SELECTED_LEASE_PROCESS")
	if mode == "" {
		return
	}
	if mode == "descendant" {
		if f := os.NewFile(3, "must-not-inherit"); f != nil {
			if info, err := f.Stat(); err == nil {
				if expected, err := os.Stat(os.Getenv("DEVKIT_TEST_SELECTED_LEASE_PATH")); err == nil && os.SameFile(info, expected) {
					t.Fatal("helper inherited controller lifecycle descriptor")
				}
			}
		}
		return
	}
	p := nativeplan.Plan{Agent: nativeagent.Spec{ID: nativeagent.ID{Project: "dev-all", Repo: "ouroboros-ide", Index: 5}, HostHome: os.Getenv("DEVKIT_TEST_SELECTED_LEASE_HOME")},
		GUITargetConfig: &nativeplan.GUITargetConfigProjection{TargetID: "fixture-local-5", Kind: "local-wsl-devkit-agent"}}
	lease, err := openSelectedControllerLifecycleLeaseAt(p, 3, os.Getenv("DEVKIT_TEST_SELECTED_LEASE_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.close()
	child := exec.Command(os.Args[0], "-test.run=^TestSelectedStagingLifecycleDescriptorSubprocess$")
	child.Env = append(os.Environ(), "DEVKIT_TEST_SELECTED_LEASE_PROCESS=descendant")
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("descriptor escaped to helper: %v %s", err, output)
	}
	fmt.Println("SELECTED-LEASE-READY")
	if mode == "cancel" {
		time.Sleep(time.Hour)
	}
}

func TestSelectedStagingRealExtraFilesCloseAndCancellationKeepParentLease(t *testing.T) {
	for _, mode := range []string{"exit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			p, _, root, path, parent := testSelectedControllerLease(t)
			if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSelectedStagingLifecycleDescriptorSubprocess$")
			cmd.ExtraFiles = []*os.File{parent}
			cmd.Env = append(os.Environ(), "DEVKIT_TEST_SELECTED_LEASE_PROCESS="+mode,
				"DEVKIT_TEST_SELECTED_LEASE_HOME="+p.Agent.HostHome, "DEVKIT_TEST_SELECTED_LEASE_ROOT="+root,
				"DEVKIT_TEST_SELECTED_LEASE_PATH="+path)
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			ready, err := bufio.NewReader(pipe).ReadString('\n')
			if err != nil || ready != "SELECTED-LEASE-READY\n" {
				cancel()
				cmd.Wait()
				t.Fatalf("real inherited fd did not verify: %q %v", ready, err)
			}
			if mode == "cancel" {
				cancel()
			}
			err = cmd.Wait()
			if mode == "exit" && err != nil {
				t.Fatal(err)
			}
			if mode == "cancel" && err == nil {
				t.Fatal("cancellation did not terminate child")
			}
			probe, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()
			if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
				t.Fatal("child exit or cancellation unlocked parent lifecycle")
			}
		})
	}
}
