package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productsession"
)

func main() {
	attestation, err := productadapter.AttestInitialNamespaces(productadapter.RoleSSHSession)
	if err == nil {
		err = run(os.Args[1:], attestation)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-ssh-session:", err)
		os.Exit(2)
	}
}

func run(args []string, attestation productadapter.Attestation) error {
	count, index, invoked, err := parseLoginShellInvocation(args)
	if err != nil {
		return err
	}
	if os.Getenv("SSH_CONNECTION") == "" || os.Getenv("SSH_TTY") != "" {
		return fmt.Errorf("Product SSH session requires a non-TTY sshd forced-command context")
	}
	original, exists := os.LookupEnv("SSH_ORIGINAL_COMMAND")
	if !exists {
		return fmt.Errorf("SSH_ORIGINAL_COMMAND is absent; interactive shells are forbidden")
	}
	parsed, err := productsession.ParseOriginalCommand(original)
	if err != nil {
		return err
	}
	authority, err := productadapter.Load(productadapter.RoleSSHSession, index, attestation)
	if err != nil {
		return err
	}
	defer authority.Close()
	expectedInvocation := strings.Join([]string{
		authority.Adapter.SSHSessionExecutablePath,
		"force-command", "--count", strconv.Itoa(count), "--index", strconv.Itoa(index),
	}, " ")
	if invoked != expectedInvocation {
		return fmt.Errorf("login shell command does not match the manifest ForceCommand")
	}
	if count != authority.Adapter.Count {
		return fmt.Errorf("SSH session count does not match authoritative manifest")
	}
	consumer, err := authority.Consumer(index)
	if err != nil {
		return err
	}
	if err := validateControlSocket(consumer.SupervisorSocketPath, consumer.UID); err != nil {
		return err
	}
	connection, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: consumer.SupervisorSocketPath, Net: "unix"})
	if err != nil {
		return fmt.Errorf("connect package-owned Product supervisor: %w", err)
	}
	defer connection.Close()
	request := productsession.Request{
		SchemaVersion:   productsession.RequestSchema,
		Count:           count,
		Index:           index,
		OriginalCommand: original,
	}
	if err := productsession.WriteFrame(connection, request); err != nil {
		return err
	}
	var response productsession.Response
	if err := productsession.ReadFrame(connection, &response); err != nil {
		return err
	}
	if response.SchemaVersion != productsession.ResponseSchema || !reflect.DeepEqual(response.Command, parsed) {
		return fmt.Errorf("Product supervisor response does not match the normalized request")
	}
	if response.Failure != nil {
		if !response.Failure.Valid() {
			return fmt.Errorf("Product supervisor returned invalid typed failure")
		}
		if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
			return err
		}
		return fmt.Errorf(
			"Product supervisor %s at %s: %s",
			response.Failure.Code,
			response.Failure.Phase,
			response.Failure.Message,
		)
	}
	if response.Status.SchemaVersion != productsession.StatusSchema || response.Status.ConsumerIndex != index {
		return fmt.Errorf("Product supervisor returned invalid typed status")
	}
	switch parsed.Kind {
	case productsession.KindPrepare:
		return json.NewEncoder(os.Stdout).Encode(response)
	case productsession.KindListen:
		if !response.Status.AppServerRunning {
			return fmt.Errorf("managed Product app-server did not start")
		}
		return nil
	case productsession.KindVersion:
		_, err := io.WriteString(os.Stdout, response.Output)
		return err
	case productsession.KindProxy:
		if !response.Status.AppServerRunning {
			return fmt.Errorf("managed Product app-server is not running")
		}
		return relay(connection, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unsupported normalized Product command")
	}
}

func parseLoginShellInvocation(args []string) (count, index int, invoked string, err error) {
	if len(args) != 2 || args[0] != "-c" {
		return 0, 0, "", fmt.Errorf("Product account shell accepts only sshd -c ForceCommand")
	}
	invoked = args[1]
	fields := strings.Split(invoked, " ")
	if len(fields) != 6 || fields[0] == "" || fields[1] != "force-command" ||
		fields[2] != "--count" || fields[4] != "--index" {
		return 0, 0, "", fmt.Errorf("Product account shell ForceCommand has invalid grammar")
	}
	count, index, err = productadapter.ParseCanonicalIdentity(fields[3], fields[5])
	if err != nil {
		return 0, 0, "", err
	}
	return count, index, invoked, nil
}

func validateControlSocket(path string, uid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Product supervisor socket: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || int(stat.Uid) != uid {
		return fmt.Errorf("Product supervisor socket has unsafe identity")
	}
	return nil
}

func relay(connection *net.UnixConn, input io.Reader, output io.Writer) error {
	type copyResult struct {
		input bool
		err   error
	}
	results := make(chan copyResult, 2)
	go func() {
		_, err := io.Copy(connection, input)
		_ = connection.CloseWrite()
		results <- copyResult{input: true, err: err}
	}()
	go func() {
		_, err := io.Copy(output, connection)
		results <- copyResult{err: err}
	}()

	first := <-results
	if !first.input {
		// A supervisor shutdown is terminal even while sshd still holds stdin
		// open. A blocking pipe read is not guaranteed to return merely because
		// another goroutine closes its descriptor, so do not join that copier:
		// close the local handles and let the forced-command process exit.
		_ = connection.Close()
		if closer, ok := input.(io.Closer); ok {
			_ = closer.Close()
		}
		return relayCopyError(first.err)
	}

	// Client EOF is only a half-close: retain the server read half so its final
	// response can drain before the forced-command process exits.
	second := <-results
	_ = connection.Close()
	if closer, ok := input.(io.Closer); ok {
		_ = closer.Close()
	}
	return errors.Join(relayCopyError(first.err), relayCopyError(second.err))
}

func relayCopyError(err error) error {
	if err != nil {
		if err != nil &&
			!errors.Is(err, net.ErrClosed) &&
			!errors.Is(err, os.ErrClosed) &&
			!errors.Is(err, io.ErrClosedPipe) {
			return err
		}
	}
	return nil
}
