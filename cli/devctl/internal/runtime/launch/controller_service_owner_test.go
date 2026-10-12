package launch

import (
	"devkit/cli/devctl/internal/runtime/agent"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

func ownerClientFixture(t *testing.T) nativeplan.Plan {
	t.Helper()
	root := t.TempDir()
	oldConfig, oldStatic, oldSocket, oldStore := nativeplan.ControllerServiceOwnerConfig, nativeplan.ControllerServiceOwnerStaticConfig, nativeplan.ControllerServiceOwnerSocket, nativeplan.ControllerProfileStoreRoot
	oldUID, oldGroup := controllerServiceOwnerUID, controllerServiceOwnerGroup
	t.Cleanup(func() {
		nativeplan.ControllerServiceOwnerConfig, nativeplan.ControllerServiceOwnerStaticConfig, nativeplan.ControllerServiceOwnerSocket, nativeplan.ControllerProfileStoreRoot = oldConfig, oldStatic, oldSocket, oldStore
		controllerServiceOwnerUID, controllerServiceOwnerGroup = oldUID, oldGroup
	})
	group, err := user.LookupGroupId(strconv.Itoa(os.Getegid()))
	if err != nil {
		t.Fatal(err)
	}
	controllerServiceOwnerUID, controllerServiceOwnerGroup = os.Geteuid(), group.Name
	nativeplan.ControllerProfileStoreRoot = filepath.Join(root, "store")
	if err := os.MkdirAll(nativeplan.ControllerProfileStoreRoot, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(nativeplan.ControllerProfileStoreRoot, "role.json")
	if err := os.WriteFile(source, []byte(`{"schemaVersion":1,"peerUser":"bayesartre","ownerUser":"root"}`), 0444); err != nil {
		t.Fatal(err)
	}
	nativeplan.ControllerServiceOwnerConfig, nativeplan.ControllerServiceOwnerStaticConfig = filepath.Join(root, "owner.json"), filepath.Join(root, "static.json")
	for _, p := range []string{nativeplan.ControllerServiceOwnerConfig, nativeplan.ControllerServiceOwnerStaticConfig} {
		if err := os.Symlink(source, p); err != nil {
			t.Fatal(err)
		}
	}
	parent := filepath.Join(root, "runtime")
	if err := os.Mkdir(parent, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0750); err != nil {
		t.Fatal(err)
	}
	nativeplan.ControllerServiceOwnerSocket = filepath.Join(parent, "control.sock")
	listener, err := net.Listen("unix", nativeplan.ControllerServiceOwnerSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := os.Chmod(nativeplan.ControllerServiceOwnerSocket, 0770); err != nil {
		t.Fatal(err)
	}
	return nativeplan.Plan{ControllerProfile: nativeplan.ManagementControllerProfileIdentity, Agent: agent.Spec{ID: agent.ID{Project: "dev-workspace", Index: 1, Repo: "shadow-throne-management"}}, Binds: []nativeplan.Bind{
		{Source: nativeplan.ControllerServiceOwnerSocket, Target: nativeplan.ControllerServiceOwnerSocket, Mode: "ro", Required: true},
		{Source: nativeplan.ControllerServiceOwnerConfig, Target: nativeplan.ControllerServiceOwnerConfig, Mode: "ro", Required: true},
	}}
}

func TestControllerOwnerClientRejectsWrongLaneWritableOrForeignBinding(t *testing.T) {
	p := ownerClientFixture(t)
	if err := validateControllerServiceOwnerClientPlan(p); err != nil {
		t.Fatal(err)
	}
	p.Agent.ID.Index = 2
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("Source2 received Runtime owner grant")
	}
	p.Agent.ID.Index = 1
	p.Binds[0].Mode = "rw"
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("writable projection admitted")
	}
	p.Binds[0].Mode = "ro"
	p.Binds = p.Binds[:1]
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("socket without immutable peer config admitted")
	}
}

func TestControllerOwnerClientRejectsMutableParentOrWrongSocketOwner(t *testing.T) {
	p := ownerClientFixture(t)
	parent := filepath.Dir(nativeplan.ControllerServiceOwnerSocket)
	if err := os.Chmod(parent, 0770); err != nil {
		t.Fatal(err)
	}
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("peer-writable parent admitted")
	}
	if err := os.Chmod(parent, 0750); err != nil {
		t.Fatal(err)
	}
	controllerServiceOwnerUID++
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("foreign socket owner admitted")
	}
	controllerServiceOwnerUID--
	if err := os.Chmod(nativeplan.ControllerServiceOwnerSocket, 0777); err != nil {
		t.Fatal(err)
	}
	if err := validateControllerServiceOwnerClientPlan(p); err == nil {
		t.Fatal("world-write socket admitted")
	}
}
