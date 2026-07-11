package main

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestStripVersionPrefix(t *testing.T) {
	cases := map[string]string{
		"/_ping":                    "/_ping",
		"/v1.41/_ping":              "/_ping",
		"/v999.1/containers/create": "/containers/create",
	}

	for input, expected := range cases {
		if got := stripVersionPrefix(input); got != expected {
			t.Fatalf("expected %s got %s", expected, got)
		}
	}
}

func TestAuthorizeContainerCreate_AllowsWhitelistedImage(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	body := []byte(`{"Image":"postgres:latest","HostConfig":{}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksOtherImages(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	body := []byte(`{"Image":"redis:latest","HostConfig":{}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestAuthorizeContainerCreate_AllowsMinioPorts(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"minio/minio:latest"}, true)}
	body := []byte(`{"Image":"minio/minio:latest","HostConfig":{"PortBindings":{"9000/tcp":[{"HostPort":""}],"9001/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukPort(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukPrivilegedMode(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukBrokerSocketBind(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_AllowsRyukSandboxBrokerSocketAlias(t *testing.T) {
	rc := &requestContext{
		policy:            mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock:        "/home/bayesartre/dev/.devkit/native-broker/broker.sock",
		brokerSockAliases: []string{"/workspaces/dev/.devkit/native-broker/broker.sock"},
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksPrivilegedModeForOtherImages(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	body := []byte(`{"Image":"postgres:latest","HostConfig":{"Privileged":true}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for privileged non-Ryuk container")
	}
}

func TestAuthorizeContainerCreate_BlocksRawDockerSocketBindForRyuk(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/var/run/docker.sock:/var/run/docker.sock:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for raw Docker socket bind")
	}
}

func TestAuthorizeContainerCreate_BlocksExtraRyukBind(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"Privileged":true,"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw","/tmp:/tmp:rw"],"PortBindings":{"8080/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for extra Ryuk bind")
	}
}

func TestAuthorizeContainerCreate_BlocksBrokerSocketBindForOtherImages(t *testing.T) {
	rc := &requestContext{
		policy:     mustPolicy(t, []string{"postgres:latest"}, true),
		brokerSock: "/workspaces/dev/.devkit/native-broker/broker.sock",
	}
	body := []byte(`{"Image":"postgres:latest","HostConfig":{"Binds":["/workspaces/dev/.devkit/native-broker/broker.sock:/var/run/docker.sock:rw"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for non-Ryuk broker socket bind")
	}
}

func TestAuthorizeContainerCreate_AllowsSAMPythonBuildBinds(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonWritableSource(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:rw","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for writable source bind")
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonOutputOutsideRepo(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/tmp/db_credentials_rotation_lambda:/out"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for output outside repo")
	}
}

func TestAuthorizeContainerCreate_BlocksSAMPythonExtraBind(t *testing.T) {
	image := "public.ecr.aws/sam/build-python3.12@sha256:e1c008f2626266024c538c9c31cbeb122e9c1c42e1a3ccd4e7b1380f6e9b7497"
	rc := &requestContext{policy: mustPolicy(t, []string{image}, true)}
	body := []byte(`{"Image":"` + image + `","HostConfig":{"Binds":["/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/modules/db_credentials_rotation/lambda_artifact:/src:ro","/workspaces/dev/agent-worktrees/agent2/ouroboros-terraform-track3-rotation-encryption-prep-20260711/.local/db_credentials_rotation_lambda:/out","/tmp:/tmp:rw"]}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block for extra SAM bind")
	}
}

func TestAuthorizeContainerCreate_BlocksRyukHostPortMismatch(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}
	body := []byte(`{"Image":"testcontainers/ryuk:0.7.0","HostConfig":{"PortBindings":{"8080/tcp":[{"HostPort":"18080"}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block when host port is forced")
	}
}

func TestAuthorizeContainerCreate_BlocksMinioHostPortOverride(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"minio/minio:latest"}, true)}
	body := []byte(`{"Image":"minio/minio:latest","HostConfig":{"PortBindings":{"9000/tcp":[{"HostPort":"1234"}],"9001/tcp":[{"HostPort":""}]}}}`)
	req, err := http.NewRequest(http.MethodPost, "http://unix/containers/create", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorizeContainerCreate(req); err == nil {
		t.Fatal("expected block when host port is forced")
	}
}

func TestAuthorizeAllowsNetworkList(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	req, err := http.NewRequest(http.MethodGet, "http://unix/networks", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorize(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeAllowsNetworkInspectByID(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}
	req, err := http.NewRequest(http.MethodGet, "http://unix/networks/04209c657e69", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := rc.authorize(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_AllowsWhitelistedPull(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, true)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=postgres&tag=latest"}

	if err := rc.authorizeImageCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_AllowsRyukPull(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"testcontainers/ryuk:0.7.0"}, true)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=testcontainers/ryuk&tag=0.7.0"}

	if err := rc.authorizeImageCreate(req); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestAuthorizeImageCreate_BlocksWhenDisabled(t *testing.T) {
	rc := &requestContext{policy: mustPolicy(t, []string{"postgres:latest"}, false)}

	req, _ := http.NewRequest(http.MethodPost, "http://unix/images/create", nil)
	req.URL = &url.URL{RawQuery: "fromImage=postgres&tag=latest"}

	if err := rc.authorizeImageCreate(req); err == nil {
		t.Fatal("expected block")
	}
}

func TestBuildClient_Unix(t *testing.T) {
	client, target, err := buildClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || target.String() != "http://docker" {
		t.Fatalf("unexpected target: %v", target)
	}
}

func TestBuildClient_TCP(t *testing.T) {
	client, target, err := buildClient("tcp://docker:2375")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil || target.String() != "http://docker:2375" {
		t.Fatalf("unexpected target: %v", target)
	}
}

func TestPolicy_AllowsMultipleImages(t *testing.T) {
	p := mustPolicy(t, []string{"postgres:latest", "minio/minio:latest"}, true)
	if !p.matchesImage("minio/minio:latest") {
		t.Fatal("expected minio image to be allowed")
	}
	if !p.matchesNameAndTag("minio/minio", "latest") {
		t.Fatal("expected name/tag match for minio")
	}
	if p.matchesImage("redis:latest") {
		t.Fatal("expected redis to be blocked")
	}
}

func TestLoadConfig_ExplicitAllowedImagesDoNotAppendLegacyDefault(t *testing.T) {
	t.Setenv("BROKER_ALLOWED_IMAGES", "minio/minio:latest")
	unsetEnv(t, "BROKER_ALLOWED_IMAGE")
	unsetEnv(t, "BROKER_ALLOWED_TAG")

	cfg := loadConfig()
	if len(cfg.AllowedImages) != 1 || cfg.AllowedImages[0] != "minio/minio:latest" {
		t.Fatalf("allowed images = %#v", cfg.AllowedImages)
	}
}

func TestLoadConfig_AppendsExplicitLegacyImage(t *testing.T) {
	t.Setenv("BROKER_ALLOWED_IMAGES", "minio/minio:latest")
	t.Setenv("BROKER_ALLOWED_IMAGE", "postgres")
	t.Setenv("BROKER_ALLOWED_TAG", "15")

	cfg := loadConfig()
	if got, want := strings.Join(cfg.AllowedImages, ","), "minio/minio:latest,postgres:15,postgres"; got != want {
		t.Fatalf("allowed images = %q, want %q", got, want)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, ok := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestValidatePortBindings_MinioAllowed(t *testing.T) {
	bindings := map[string][]portBinding{
		"9000/tcp": []portBinding{{HostPort: ""}},
		"9001/tcp": []portBinding{{HostPort: ""}},
	}
	if err := validatePortBindings(bindings, []string{"9000/tcp", "9001/tcp"}); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestValidatePortBindings_RejectsUnexpectedPort(t *testing.T) {
	bindings := map[string][]portBinding{
		"9000/tcp": []portBinding{{HostPort: ""}},
		"1234/tcp": []portBinding{{HostPort: ""}},
	}
	if err := validatePortBindings(bindings, []string{"9000/tcp", "9001/tcp"}); err == nil {
		t.Fatal("expected block for unexpected port")
	}
}

func mustPolicy(t *testing.T, images []string, allowPull bool) *policy {
	t.Helper()
	p, err := newPolicy(images, allowPull)
	if err != nil {
		t.Fatalf("newPolicy failed: %v", err)
	}
	return p
}
