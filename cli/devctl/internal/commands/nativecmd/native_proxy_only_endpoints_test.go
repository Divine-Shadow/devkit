package nativecmd

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
	nativeplan "devkit/cli/devctl/internal/runtime/plan"
)

func testProxyOnlyEndpointPlan(t *testing.T) (nativeplan.Plan, os.FileInfo) {
	t.Helper()
	t.Setenv("DEVKIT_NATIVE_ISOLATION_PROFILE", "")
	dir := shortSocketTempDir(t)
	allowlist := filepath.Join(dir, "allowlist.txt")
	if err := os.WriteFile(allowlist, []byte("ssh.github.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "shared.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	info, err := os.Lstat(socket)
	if err != nil {
		t.Fatal(err)
	}
	return nativeplan.Plan{
		Proxy: nativeplan.ProxyConfig{UnixSocket: socket, AllowlistPath: allowlist},
		Binds: []nativeplan.Bind{{Source: socket, Target: socket, Mode: "rw", Required: true}},
	}, info
}

func assertProxyOnlySharedListenerPreserved(t *testing.T, p nativeplan.Plan, before os.FileInfo) {
	t.Helper()
	after, err := os.Lstat(p.Proxy.UnixSocket)
	if err != nil || !os.SameFile(before, after) || !unixSocketAccepts(p.Proxy.UnixSocket) {
		t.Fatalf("unverified shared listener changed: %v", err)
	}
}

func TestWithManagedRuntimeEndpointsProxyOnlyPreservesExistingListener(t *testing.T) {
	for _, callbackFails := range []bool{false, true} {
		t.Run(fmt.Sprint(callbackFails), func(t *testing.T) {
			p, shared := testProxyOnlyEndpointPlan(t)
			callbackErr := errors.New("preparation callback failed")
			seen := map[string]bool{}
			for invocation := 0; invocation < 2; invocation++ {
				var privateSocket string
				err := withManagedRuntimeEndpoints(p, runtimebroker.Config{}, false, func(got nativeplan.Plan) error {
					privateSocket = got.Proxy.UnixSocket
					if privateSocket == p.Proxy.UnixSocket || got.PostgresDockerSocket != "" ||
						got.Proxy.AllowlistPath != p.Proxy.AllowlistPath || got.Binds[0].Source != privateSocket ||
						got.Binds[0].Target != privateSocket || !got.Binds[0].Required || got.Binds[0].Mode != "rw" {
						return fmt.Errorf("proxy-only callback received wrong endpoint or policy binding")
					}
					conn, err := net.DialTimeout("unix", privateSocket, time.Second)
					if err != nil {
						return err
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(time.Second))
					if _, err := fmt.Fprint(conn, "CONNECT blocked.invalid:443 HTTP/1.1\r\nHost: blocked.invalid:443\r\n\r\n"); err != nil {
						return err
					}
					response, err := http.ReadResponse(bufio.NewReader(conn), nil)
					if err != nil {
						return err
					}
					defer response.Body.Close()
					if response.StatusCode != http.StatusForbidden {
						return fmt.Errorf("isolated egress lost allowlist enforcement: %d", response.StatusCode)
					}
					if callbackFails {
						return callbackErr
					}
					return nil
				})
				if privateSocket == "" || (callbackFails && !errors.Is(err, callbackErr)) || (!callbackFails && err != nil) {
					t.Fatalf("normal proxy-only callback not completed: private=%q err=%v", privateSocket, err)
				}
				if _, err := os.Lstat(privateSocket); !errors.Is(err, os.ErrNotExist) || unixSocketAccepts(privateSocket) {
					t.Fatalf("owned private proxy survived callback cleanup: %v", err)
				}
				assertProxyOnlySharedListenerPreserved(t, p, shared)
				if p.Binds[0].Source != p.Proxy.UnixSocket || p.Binds[0].Target != p.Proxy.UnixSocket {
					t.Fatal("input plan was mutated")
				}
				if seen[privateSocket] {
					t.Fatal("subsequent preparation reused an invocation endpoint")
				}
				seen[privateSocket] = true
			}
		})
	}
}

func TestWithManagedRuntimeEndpointsProxyOnlyOverlappingInvocations(t *testing.T) {
	p, shared := testProxyOnlyEndpointPlan(t)
	var first, second string
	err := withManagedRuntimeEndpoints(p, runtimebroker.Config{}, false, func(a nativeplan.Plan) error {
		first = a.Proxy.UnixSocket
		if !unixSocketAccepts(first) {
			return errors.New("first private endpoint absent")
		}
		err := withManagedRuntimeEndpoints(p, runtimebroker.Config{}, false, func(b nativeplan.Plan) error {
			second = b.Proxy.UnixSocket
			if first == second || second == p.Proxy.UnixSocket || !unixSocketAccepts(first) || !unixSocketAccepts(second) {
				return errors.New("overlapping proxy-only invocations shared endpoint custody")
			}
			return nil
		})
		if err != nil || !unixSocketAccepts(first) || unixSocketAccepts(second) {
			return fmt.Errorf("nested cleanup affected first endpoint: %v", err)
		}
		return nil
	})
	if err != nil || first == "" || second == "" {
		t.Fatalf("overlapping endpoint preparation failed: %v", err)
	}
	for _, socket := range []string{first, second} {
		if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("owned endpoint pathname survived: %v", err)
		}
	}
	assertProxyOnlySharedListenerPreserved(t, p, shared)
}

func TestProxyOnlyRuntimeExecutionIsolationAndRequiredBindRefusal(t *testing.T) {
	p, shared := testProxyOnlyEndpointPlan(t)
	got, err := isolateRuntimeExecutionPlan(p, false)
	if err != nil || got.Proxy.UnixSocket == p.Proxy.UnixSocket || got.Binds[0].Source != got.Proxy.UnixSocket {
		t.Fatalf("runtime execution omitted proxy-only isolation: %#v %v", got, err)
	}
	dry, err := isolateRuntimeExecutionPlan(p, true)
	if err != nil || !reflect.DeepEqual(dry, p) {
		t.Fatalf("dry run changed existing contract: %#v %v", dry, err)
	}
	for _, binds := range [][]nativeplan.Bind{nil, {p.Binds[0], p.Binds[0]}} {
		invalid := p
		invalid.Binds = binds
		called := false
		err := withManagedRuntimeEndpoints(invalid, runtimebroker.Config{}, false, func(nativeplan.Plan) error { called = true; return nil })
		if err == nil || !strings.Contains(err.Error(), "must appear exactly once") || called {
			t.Fatalf("invalid required endpoint bind admitted: called=%v err=%v", called, err)
		}
	}
	assertProxyOnlySharedListenerPreserved(t, p, shared)
}

func TestProxyOnlyRuntimeExecutionRetainedSharedEndpointTwice(t *testing.T) {
	p, shared := testProxyOnlyEndpointPlan(t)
	seen := map[string]bool{}
	for invocation := 0; invocation < 2; invocation++ {
		private, err := isolateRuntimeExecutionPlan(p, false)
		if err != nil || private.Proxy.UnixSocket == p.Proxy.UnixSocket || seen[private.Proxy.UnixSocket] {
			t.Fatalf("execution did not get an independent endpoint: %v", err)
		}
		seen[private.Proxy.UnixSocket] = true
		called := false
		err = withManagedEgressProxy(private, false, func() error {
			called = true
			if !unixSocketAccepts(private.Proxy.UnixSocket) || !unixSocketAccepts(p.Proxy.UnixSocket) {
				return errors.New("execution proxy or retained shared listener unavailable")
			}
			return nil
		})
		if err != nil || !called {
			t.Fatalf("normal execution endpoint lifecycle failed: %v", err)
		}
		if _, err := os.Lstat(private.Proxy.UnixSocket); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("execution endpoint survived its invocation: %v", err)
		}
		assertProxyOnlySharedListenerPreserved(t, p, shared)
	}
}
