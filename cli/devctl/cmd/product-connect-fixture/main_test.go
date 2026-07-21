package main

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
)

func TestExactLoopbackEndpoint(t *testing.T) {
	for _, invalid := range []string{"", "localhost:1", "0.0.0.0:1", "127.0.0.1", "[127.0.0.1]:1"} {
		if _, err := exactLoopbackEndpoint(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if got, err := exactLoopbackEndpoint("127.0.0.1:18080"); err != nil || got != "127.0.0.1:18080" {
		t.Fatalf("canonical endpoint rejected: %q %v", got, err)
	}
}

func TestServeConnectionRelaysOnlyExactFixtureTarget(t *testing.T) {
	upstreamListener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamListener.Close()
	go func() {
		connection, acceptErr := upstreamListener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		payload := make([]byte, len("fixture"))
		_, _ = io.ReadFull(connection, payload)
		_, _ = connection.Write([]byte(strings.ToUpper(string(payload))))
	}()

	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- serveConnection(context.Background(), server, upstreamListener.Addr().String())
	}()
	request := "CONNECT ssh.github.com:443 HTTP/1.1\r\nHost: ssh.github.com:443\r\n\r\n"
	if _, err := io.WriteString(client, request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if _, err := io.ReadFull(client, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "HTTP/1.1 200 Connection Established\r\n\r\n" {
		t.Fatalf("unexpected response %q", response)
	}
	if _, err := client.Write([]byte("fixture")); err != nil {
		t.Fatal(err)
	}
	result := make([]byte, len("FIXTURE"))
	if _, err := io.ReadFull(client, result); err != nil {
		t.Fatal(err)
	}
	if string(result) != "FIXTURE" {
		t.Fatalf("unexpected tunneled result %q", result)
	}
	_ = client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeConnectionRejectsOtherTarget(t *testing.T) {
	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- serveConnection(context.Background(), server, "127.0.0.1:1") }()
	_, _ = io.WriteString(client, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(response), "HTTP/1.1 403 Forbidden") {
		t.Fatalf("unexpected refusal %q", response)
	}
	if err := <-done; err == nil {
		t.Fatal("wrong target was accepted")
	}
}
