package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const (
	exactTarget  = "ssh.github.com:443"
	requestLimit = 16 << 10
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fail(err.Error())
	}
}

func run(args []string) error {
	if len(args) != 5 || args[0] != "serve" || args[1] != "--listen" || args[3] != "--upstream" {
		return fmt.Errorf("usage: product-connect-fixture serve --listen 127.0.0.1:PORT --upstream 127.0.0.1:PORT")
	}
	listen, err := exactLoopbackEndpoint(args[2])
	if err != nil {
		return fmt.Errorf("listen endpoint: %w", err)
	}
	upstream, err := exactLoopbackEndpoint(args[4])
	if err != nil {
		return fmt.Errorf("upstream endpoint: %w", err)
	}
	listener, err := net.Listen("tcp4", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go func() {
			_ = serveConnection(ctx, connection, upstream)
		}()
	}
}

func exactLoopbackEndpoint(raw string) (string, error) {
	host, port, err := net.SplitHostPort(raw)
	if err != nil || host != "127.0.0.1" || port == "" {
		return "", fmt.Errorf("requires exact 127.0.0.1:PORT")
	}
	joined := net.JoinHostPort(host, port)
	if joined != raw {
		return "", fmt.Errorf("endpoint is not byte-canonical")
	}
	return joined, nil
}

func serveConnection(ctx context.Context, client net.Conn, upstreamEndpoint string) error {
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(io.LimitReader(client, requestLimit+1))
	request, err := http.ReadRequest(reader)
	if err != nil {
		return err
	}
	if request.Method != http.MethodConnect || request.RequestURI != exactTarget || request.Host != exactTarget || request.ContentLength > 0 {
		_, _ = io.WriteString(client, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
		return fmt.Errorf("refused non-fixture CONNECT target")
	}
	upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", upstreamEndpoint)
	if err != nil {
		return err
	}
	defer upstream.Close()
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return err
	}
	_ = client.SetReadDeadline(time.Time{})
	return relay(client, upstream)
}

func relay(left, right net.Conn) error {
	var wait sync.WaitGroup
	errorsOut := make(chan error, 2)
	copyOne := func(destination, source net.Conn) {
		defer wait.Done()
		_, err := io.Copy(destination, source)
		if tcp, ok := destination.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		errorsOut <- err
	}
	wait.Add(2)
	go copyOne(left, right)
	go copyOne(right, left)
	wait.Wait()
	close(errorsOut)
	var result error
	for err := range errorsOut {
		if err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func fail(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "product-connect-fixture:", message)
	os.Exit(2)
}
