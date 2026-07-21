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
	"sync"
	"syscall"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productsession"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-ssh-session:", err)
		os.Exit(2)
	}
}

func run(args []string) error {
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
	authority, err := productadapter.Load(productadapter.RoleSSHSession, index)
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
	if response.Error != "" {
		return errors.New(response.Error)
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
	var wait sync.WaitGroup
	errorsOut := make(chan error, 2)
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, err := io.Copy(connection, input)
		_ = connection.CloseWrite()
		errorsOut <- err
	}()
	go func() {
		defer wait.Done()
		_, err := io.Copy(output, connection)
		errorsOut <- err
	}()
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
