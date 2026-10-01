package launch

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostgresEndpointPingCompleteBoundedResponse(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		valid, hold    bool
	}{
		{"complete", "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK", true, false},
		{"oversized-prefix", "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\nOK" + strings.Repeat(" ", 31), false, true},
		{"incomplete-small", "HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nOK", false, true},
		{"chunked-complete", "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n2\r\nOK\r\n0\r\n\r\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := os.MkdirTemp("", "pg-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			socket := filepath.Join(root, "ping.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			release := make(chan struct{})
			defer close(release)
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					done <- err
					return
				}
				if request.URL.Path != "/_ping" {
					done <- fmt.Errorf("unexpected request %s", request.URL.Path)
					return
				}
				_, err = io.WriteString(conn, tc.response)
				done <- err
				if tc.hold {
					<-release
				}
			}()
			before := time.Now()
			err = postgresEndpointPing(socket)
			if tc.valid && err != nil {
				t.Fatalf("complete response rejected: %v", err)
			}
			if !tc.valid && err == nil {
				t.Fatal("incomplete or oversized HTTP body admitted")
			}
			if time.Since(before) > time.Second {
				t.Fatal("ping exceeded bounded response deadline")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
