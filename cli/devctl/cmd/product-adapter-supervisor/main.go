package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"devkit/cli/devctl/internal/productadapter"
	"devkit/cli/devctl/internal/productsession"
)

type supervisor struct {
	authority productadapter.Authority
	consumer  productadapter.ConsumerManifest
	count     int
	index     int

	mu                  sync.Mutex
	command             *exec.Cmd
	done                chan error
	processGroup        int
	commandStartTime    uint64
	appServerPID        int
	appServerStartTime  uint64
	appServerSocket     socketOwnership
	startCount          int
	supervisorStartTime uint64
	consumerCgroup      string
}

var errSocketNotReady = errors.New("Product app-server socket is not listening yet")

func main() {
	attestation, err := productadapter.AttestInitialNamespaces(productadapter.RoleSupervisor)
	if err == nil {
		err = run(os.Args[1:], attestation)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "product-adapter-supervisor:", err)
		os.Exit(2)
	}
}

func run(args []string, attestation productadapter.Attestation) error {
	if len(args) != 5 || args[0] != "serve" || args[1] != "--count" || args[3] != "--index" {
		return fmt.Errorf("requires exactly: product-adapter-supervisor serve --count N --index N")
	}
	count, index, err := productadapter.ParseCanonicalIdentity(args[2], args[4])
	if err != nil {
		return err
	}
	authority, err := productadapter.Load(productadapter.RoleSupervisor, index, attestation)
	if err != nil {
		return err
	}
	defer authority.Close()
	if count != authority.Adapter.Count {
		return fmt.Errorf("supervisor count does not match authoritative manifest")
	}
	consumer, err := authority.Consumer(index)
	if err != nil {
		return err
	}
	startTime, err := processStartTime(os.Getpid())
	if err != nil {
		return err
	}
	cgroup, err := processCgroup(os.Getpid())
	if err != nil {
		return err
	}
	service := &supervisor{
		authority:           authority,
		consumer:            consumer,
		count:               count,
		index:               index,
		supervisorStartTime: startTime,
		consumerCgroup:      cgroup,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return service.serve(ctx)
}

func (service *supervisor) serve(ctx context.Context) (result error) {
	if err := validateControlParent(filepath.Dir(service.consumer.SupervisorSocketPath), service.consumer.UID); err != nil {
		return err
	}
	if _, err := os.Lstat(service.consumer.SupervisorSocketPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("Product supervisor refuses preexisting control socket")
		}
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: service.consumer.SupervisorSocketPath, Net: "unix"})
	if err != nil {
		return err
	}
	if err := os.Chmod(service.consumer.SupervisorSocketPath, 0o600); err != nil {
		listener.Close()
		return err
	}
	socket, err := pathSocketIdentity(service.consumer.SupervisorSocketPath)
	if err != nil {
		listener.Close()
		return err
	}
	var handlers sync.WaitGroup
	var activeConnections sync.Map
	defer func() {
		_ = listener.Close()
		shutdownErr := drainSupervisorHandlers(service.stopTarget, &activeConnections, &handlers)
		socketErr := removeExactSocket(service.consumer.SupervisorSocketPath, socket)
		result = errors.Join(result, shutdownErr, socketErr)
	}()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := requirePeerUID(connection, service.consumer.UID); err != nil {
			_ = connection.Close()
			continue
		}
		activeConnections.Store(connection, struct{}{})
		handlers.Add(1)
		go func() {
			defer handlers.Done()
			defer activeConnections.Delete(connection)
			defer connection.Close()
			service.handle(ctx, connection)
		}()
	}
}

func drainSupervisorHandlers(
	stopTarget func() error,
	activeConnections *sync.Map,
	handlers *sync.WaitGroup,
) error {
	stopErr := stopTarget()
	activeConnections.Range(func(connection, _ any) bool {
		if unix, ok := connection.(*net.UnixConn); ok {
			_ = unix.Close()
		}
		return true
	})
	handlers.Wait()
	return stopErr
}

func (service *supervisor) handle(ctx context.Context, connection *net.UnixConn) {
	var request productsession.Request
	if err := productsession.ReadFrame(connection, &request); err != nil {
		return
	}
	parsed, err := productsession.ParseOriginalCommand(request.OriginalCommand)
	failureOperation := productsession.FailureRequest
	if request.SchemaVersion != productsession.RequestSchema || request.Count != service.count || request.Index != service.index {
		err = fmt.Errorf("Product supervisor request does not match authoritative identity")
	} else if err == nil {
		failureOperation = productsession.FailureOperationFor(parsed)
	}
	respond := func(responseError error, output string) bool {
		status := service.status()
		if responseError != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"product-adapter-supervisor: operation %s failed: %v\n",
				parsed.Kind,
				responseError,
			)
		} else if parsed.Kind == productsession.KindListen {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"product-adapter-supervisor: operation %s completed: running=%t pid=%d process_count=%d start_count=%d\n",
				parsed.Kind,
				status.AppServerRunning,
				status.AppServerPID,
				status.AppServerProcessCount,
				status.StartCount,
			)
		}
		response := productsession.Response{
			SchemaVersion: productsession.ResponseSchema,
			Command:       parsed,
			Status:        status,
			Output:        output,
		}
		if responseError != nil {
			response.Output = ""
			response.Failure = productsession.FailureFor(failureOperation, responseError)
		}
		return productsession.WriteFrame(connection, response) == nil
	}
	if err != nil {
		respond(err, "")
		return
	}
	switch parsed.Kind {
	case productsession.KindPrepare:
		err = service.prepare(ctx)
		respond(err, "")
	case productsession.KindListen:
		err = service.ensureTarget(ctx)
		respond(err, "")
	case productsession.KindVersion:
		var output []byte
		output, err = service.codexVersion(ctx)
		respond(err, string(output))
	case productsession.KindProxy:
		if err = service.ensureTarget(ctx); err != nil {
			respond(err, "")
			return
		}
		var upstream *net.UnixConn
		upstream, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: service.consumer.AppServerSocketPath, Net: "unix"})
		if err != nil || !respond(err, "") {
			if upstream != nil {
				upstream.Close()
			}
			return
		}
		defer upstream.Close()
		_ = relayUnix(connection, upstream)
	}
}

func (service *supervisor) prepare(ctx context.Context) error {
	if _, err := os.Lstat(service.consumer.CandidateRoot); err == nil {
		if _, receiptErr := os.Lstat(service.consumer.ReceiptPath); receiptErr == nil {
			receipt, readErr := productadapter.ReadReceipt(service.authority, service.count, service.index)
			if readErr != nil {
				return fmt.Errorf("existing Product candidate is not reusable canonical state: %w", readErr)
			}
			appServerRunning := false
			if receipt.Status == "executing" {
				service.mu.Lock()
				appServerRunning = service.targetHealthyLocked()
				service.mu.Unlock()
			}
			if !candidateStateReusable(receipt.Status, appServerRunning) {
				return fmt.Errorf("existing Product candidate is neither ready nor exact healthy execution")
			}
			return nil
		} else if !errors.Is(receiptErr, os.ErrNotExist) {
			return receiptErr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	adapterLaunch, err := productadapter.LaunchExecutable(productadapter.RoleAdapter)
	if err != nil {
		return err
	}
	command := exec.CommandContext(
		ctx,
		adapterLaunch,
		"prepare", "--count", strconv.Itoa(service.count), "--index", strconv.Itoa(service.index),
	)
	command.Env = []string{}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := command.Run(); err != nil {
		return fmt.Errorf("canonical Product preparation failed: %w", err)
	}
	receipt, err := productadapter.ReadReceipt(service.authority, service.count, service.index)
	if err != nil || receipt.Status != "ready" {
		return fmt.Errorf("canonical Product preparation did not produce a ready receipt: %w", err)
	}
	return nil
}

func candidateStateReusable(receiptStatus string, appServerRunning bool) bool {
	return receiptStatus == "ready" || (receiptStatus == "executing" && appServerRunning)
}

func (service *supervisor) ensureTarget(ctx context.Context) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.targetHealthyLocked() {
		return nil
	}
	if service.command != nil {
		_ = service.stopTargetLocked()
	}
	if _, err := productadapter.ReadReceipt(service.authority, service.count, service.index); err != nil {
		return fmt.Errorf("managed Product app-server requires a ready canonical consumer: %w", err)
	}
	if processes, err := findConsumerAppServers(
		service.authority.Adapter.CodexExecutablePath,
		service.consumer.UID,
		service.consumerCgroup,
		service.consumer.SandboxAppServerSocketPath,
	); err != nil {
		return err
	} else if len(processes) != 0 {
		return fmt.Errorf("managed Product app-server refuses %d preexisting pinned app-server process(es) in the consumer boundary", len(processes))
	}
	if _, err := os.Lstat(service.consumer.AppServerSocketPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("managed Product app-server refuses preexisting socket path")
		}
		return err
	}
	adapterLaunch, err := productadapter.LaunchExecutable(productadapter.RoleAdapter)
	if err != nil {
		return err
	}
	command := exec.Command(
		adapterLaunch,
		"exec", "--count", strconv.Itoa(service.count), "--index", strconv.Itoa(service.index), "--",
		service.authority.Adapter.CodexExecutablePath,
		"-c", "features.code_mode_host=true",
		"app-server", "--listen", "unix://"+service.consumer.SandboxAppServerSocketPath,
	)
	command.Env = []string{}
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := command.Start(); err != nil {
		return err
	}
	service.command = command
	service.processGroup = command.Process.Pid
	service.done = make(chan error, 1)
	go func() { service.done <- command.Wait() }()
	commandStartTime, err := processStartTime(command.Process.Pid)
	if err != nil {
		_ = service.stopTargetLocked()
		return fmt.Errorf("capture managed Product command identity: %w", err)
	}
	service.commandStartTime = commandStartTime
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-service.done:
			service.command = nil
			return fmt.Errorf("managed Product app-server exited before readiness: %w", err)
		default:
		}
		processes, scanErr := findConsumerAppServers(
			service.authority.Adapter.CodexExecutablePath,
			service.consumer.UID,
			service.consumerCgroup,
			service.consumer.SandboxAppServerSocketPath,
		)
		if scanErr != nil {
			_ = service.stopTargetLocked()
			return scanErr
		}
		if len(processes) > 1 {
			_ = service.stopTargetLocked()
			return fmt.Errorf("managed Product app-server produced a duplicate or lookalike process")
		}
		if len(processes) == 1 {
			if !processes[0].ExactTarget && !isExactReadinessAppServer(processes[0].Arguments) {
				_ = service.stopTargetLocked()
				return fmt.Errorf(
					"managed Product app-server produced noncanonical argv %q",
					processes[0].Arguments,
				)
			}
			// The immutable adapter validates the prepared consumer through one
			// exact stdio app-server before it starts the managed listener. The
			// readiness process deliberately owns a nested process group so it can
			// clean up all of its descendants. Prove that transient instead by an
			// unbroken ancestry chain to this exact, newly started adapter process.
			// It is never accepted as the target and must disappear before the
			// listener socket/process predicate can succeed.
			if !processes[0].ExactTarget {
				owned, ancestryErr := processRootedAt(
					processes[0].PID,
					service.command.Process.Pid,
					service.commandStartTime,
				)
				if ancestryErr != nil {
					if errors.Is(ancestryErr, os.ErrNotExist) {
						continue
					}
					_ = service.stopTargetLocked()
					return fmt.Errorf("prove Product readiness app-server ancestry: %w", ancestryErr)
				}
				if !owned {
					_ = service.stopTargetLocked()
					return fmt.Errorf("Product readiness app-server is outside the newly started adapter process tree")
				}
				select {
				case <-ctx.Done():
					_ = service.stopTargetLocked()
					return ctx.Err()
				case <-time.After(50 * time.Millisecond):
				}
				continue
			}
			group, groupErr := syscall.Getpgid(processes[0].PID)
			if groupErr != nil || group != service.processGroup {
				_ = service.stopTargetLocked()
				return fmt.Errorf(
					"managed Product app-server escaped its declared process group: pid=%d observed=%d expected=%d argv=%q lookup=%v",
					processes[0].PID,
					group,
					service.processGroup,
					processes[0].Arguments,
					groupErr,
				)
			}
			startTime, startErr := processStartTime(processes[0].PID)
			if startErr != nil {
				_ = service.stopTargetLocked()
				return startErr
			}
			socket, socketErr := proveProcessOwnsListeningUnixSocket(
				processes[0].PID,
				startTime,
				service.consumer.AppServerSocketPath,
				service.consumer.SandboxAppServerSocketPath,
			)
			if socketErr == nil && int(socket.Filesystem.Owner) == service.consumer.UID {
				connection, dialErr := net.DialTimeout("unix", service.consumer.AppServerSocketPath, 100*time.Millisecond)
				if dialErr == nil {
					connection.Close()
					verified, verifyErr := proveProcessOwnsListeningUnixSocket(
						processes[0].PID,
						startTime,
						service.consumer.AppServerSocketPath,
						service.consumer.SandboxAppServerSocketPath,
					)
					if verifyErr != nil {
						_ = service.stopTargetLocked()
						return fmt.Errorf("revalidate managed Product app-server socket ownership during acceptance: %w", verifyErr)
					}
					if verified != socket {
						_ = service.stopTargetLocked()
						return socketOwnershipChangedDuringAcceptanceError(socket, verified)
					}
					service.appServerPID = processes[0].PID
					service.appServerStartTime = startTime
					service.appServerSocket = socket
					service.startCount++
					return nil
				}
			} else if socketErr == nil {
				_ = service.stopTargetLocked()
				return fmt.Errorf("managed Product app-server socket owner is not the consumer uid")
			} else if !errors.Is(socketErr, os.ErrNotExist) && !errors.Is(socketErr, errSocketNotReady) {
				_ = service.stopTargetLocked()
				return fmt.Errorf("managed Product app-server does not own its declared listener: %w", socketErr)
			}
		}
		select {
		case <-ctx.Done():
			_ = service.stopTargetLocked()
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	_ = service.stopTargetLocked()
	return fmt.Errorf("managed Product app-server did not reach the exact socket/process predicate")
}

func (service *supervisor) targetHealthyLocked() bool {
	if service.command == nil || service.appServerPID == 0 {
		return false
	}
	startTime, err := processStartTime(service.appServerPID)
	if err != nil || startTime != service.appServerStartTime {
		return false
	}
	processes, err := findConsumerAppServers(
		service.authority.Adapter.CodexExecutablePath,
		service.consumer.UID,
		service.consumerCgroup,
		service.consumer.SandboxAppServerSocketPath,
	)
	if err != nil || len(processes) != 1 || processes[0].PID != service.appServerPID || !processes[0].ExactTarget {
		return false
	}
	socket, socketErr := proveProcessOwnsListeningUnixSocket(
		service.appServerPID,
		service.appServerStartTime,
		service.consumer.AppServerSocketPath,
		service.consumer.SandboxAppServerSocketPath,
	)
	if socketErr != nil || socket != service.appServerSocket {
		return false
	}
	connection, err := net.DialTimeout("unix", service.consumer.AppServerSocketPath, 100*time.Millisecond)
	if err != nil {
		return false
	}
	connection.Close()
	return true
}

func (service *supervisor) stopTarget() error {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.stopTargetLocked()
}

func (service *supervisor) stopTargetLocked() error {
	if service.command == nil {
		return nil
	}
	group := service.processGroup
	done := service.done
	// Terminate the exact pinned app-server first. That lets bubblewrap and the
	// compiled adapter unwind normally, so the adapter's source-defined proxy
	// cleanup and receipt transition run. Killing the whole group first would
	// bypass those defers and leave a dead Unix socket that falsely resembles a
	// live canonical consumer.
	graceful := false
	if service.appServerPID != 0 {
		if startTime, err := processStartTime(service.appServerPID); err == nil && startTime == service.appServerStartTime {
			graceful = syscall.Kill(service.appServerPID, syscall.SIGTERM) == nil
		}
	}
	if !graceful {
		_ = syscall.Kill(-group, syscall.SIGTERM)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-group, syscall.SIGKILL)
		<-done
	}
	proxyErr := error(nil)
	if _, err := os.Lstat(service.consumer.ProxySocketPath); err == nil {
		proxyErr = fmt.Errorf("managed Product adapter left proxy socket residue after target shutdown")
	} else if !errors.Is(err, os.ErrNotExist) {
		proxyErr = err
	}
	service.command = nil
	service.processGroup = 0
	service.commandStartTime = 0
	service.appServerPID = 0
	service.appServerStartTime = 0
	if service.appServerSocket != (socketOwnership{}) {
		if err := removeExactSocket(service.consumer.AppServerSocketPath, service.appServerSocket.Filesystem); err != nil {
			service.appServerSocket = socketOwnership{}
			return errors.Join(proxyErr, err)
		}
	}
	service.appServerSocket = socketOwnership{}
	return proxyErr
}

func (service *supervisor) status() productsession.Status {
	service.mu.Lock()
	defer service.mu.Unlock()
	running := service.targetHealthyLocked()
	status := productsession.Status{
		SchemaVersion:              productsession.StatusSchema,
		ManifestPath:               service.authority.ManifestPath,
		ManifestDigest:             service.authority.ManifestDigest,
		ConsumerIndex:              service.index,
		MountPolicyIdentity:        service.authority.Adapter.MountPolicyIdentity,
		MountPolicyContractPath:    service.authority.Adapter.MountPolicyContractPath,
		MountPolicyContractDigest:  service.authority.Adapter.ArtifactDigests["mount_policy_contract"],
		MCPRequirementPath:         service.authority.Adapter.MCPRequirementPath,
		MCPRequirementDigest:       service.authority.Adapter.ArtifactDigests["mcp_requirement"],
		CodexExecutablePath:        service.authority.Adapter.CodexExecutablePath,
		CodexExecutableDigest:      service.authority.Adapter.ArtifactDigests["codex_executable"],
		SupervisorPID:              os.Getpid(),
		SupervisorStartTime:        service.supervisorStartTime,
		ControlSocketPath:          service.consumer.SupervisorSocketPath,
		AppServerRunning:           running,
		AppServerSocketPath:        service.consumer.AppServerSocketPath,
		SandboxAppServerSocketPath: service.consumer.SandboxAppServerSocketPath,
		StartCount:                 service.startCount,
	}
	if processes, err := findConsumerAppServers(
		service.authority.Adapter.CodexExecutablePath,
		service.consumer.UID,
		service.consumerCgroup,
		service.consumer.SandboxAppServerSocketPath,
	); err == nil {
		status.AppServerProcessCount = len(processes)
	}
	if running {
		status.AppServerPID = service.appServerPID
		status.AppServerStartTime = service.appServerStartTime
		status.AppServerProcessGroup = service.processGroup
		status.AppServerSocketDevice = service.appServerSocket.Filesystem.Device
		status.AppServerSocketInode = service.appServerSocket.Filesystem.Inode
		status.AppServerSocketOwner = service.appServerSocket.Filesystem.Owner
		status.AppServerKernelSocketInode = service.appServerSocket.KernelInode
		status.MountNamespaceDistinct = namespacesDistinct(service.appServerPID, "mnt")
		status.NetworkNamespaceDistinct = namespacesDistinct(service.appServerPID, "net")
		status.WindowsMountsAbsent = windowsMountsAbsent(service.appServerPID)
	}
	return status
}

func (service *supervisor) codexVersion(ctx context.Context) ([]byte, error) {
	command := exec.CommandContext(ctx, service.authority.Adapter.CodexExecutablePath, "--version")
	command.Env = []string{}
	return command.Output()
}

func validateControlParent(path string, uid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect Product supervisor runtime directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || int(stat.Uid) != uid {
		return fmt.Errorf("Product supervisor runtime directory has unsafe identity")
	}
	return nil
}

func requirePeerUID(connection *net.UnixConn, uid int) error {
	raw, err := connection.SyscallConn()
	if err != nil {
		return err
	}
	var credential *syscall.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if controlErr != nil {
		return controlErr
	}
	if credential == nil || int(credential.Uid) != uid {
		return fmt.Errorf("Product supervisor refuses a peer with the wrong uid")
	}
	return nil
}

type codexProcess struct {
	PID         int
	ExactTarget bool
	Arguments   []string
}

func findConsumerAppServers(executable string, uid int, consumerCgroup, sandboxSocket string) ([]codexProcess, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	resolvedExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, fmt.Errorf("canonicalize pinned Codex executable: %w", err)
	}
	var matches []codexProcess
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		resolved, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil || strings.TrimSuffix(resolved, " (deleted)") != resolvedExecutable {
			continue
		}
		processUID, err := processEffectiveUID(pid)
		if err != nil || processUID != uid {
			continue
		}
		cgroup, err := processCgroup(pid)
		if err != nil || cgroup != consumerCgroup {
			continue
		}
		payload, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		parts := bytes.Split(bytes.TrimSuffix(payload, []byte{0}), []byte{0})
		if len(parts) < 2 {
			continue
		}
		arguments := make([]string, 0, len(parts)-1)
		for _, value := range parts[1:] {
			argument := string(value)
			arguments = append(arguments, argument)
		}
		if !containsAppServerSubcommand(arguments) {
			continue
		}
		matches = append(matches, codexProcess{
			PID:         pid,
			ExactTarget: isExactManagedAppServer(arguments, sandboxSocket),
			Arguments:   arguments,
		})
	}
	return matches, nil
}

func containsAppServerSubcommand(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "app-server" {
			return true
		}
	}
	return false
}

func isExactManagedAppServer(arguments []string, sandboxSocket string) bool {
	expected := []string{
		"-c", "features.code_mode_host=true",
		"app-server", "--listen", "unix://" + sandboxSocket,
	}
	if len(arguments) != len(expected) {
		return false
	}
	for index := range expected {
		if arguments[index] != expected[index] {
			return false
		}
	}
	return true
}

func isExactReadinessAppServer(arguments []string) bool {
	return len(arguments) == 2 && arguments[0] == "app-server" && arguments[1] == "--stdio"
}

func processEffectiveUID(pid int) (int, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 5 && fields[0] == "Uid:" {
			value, err := strconv.Atoi(fields[2])
			if err != nil {
				return 0, err
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("process status has no effective uid")
}

func processCgroup(pid int) (string, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", err
	}
	if len(payload) == 0 {
		return "", fmt.Errorf("process cgroup is empty")
	}
	return string(payload), nil
}

type processStat struct {
	ParentPID int
	StartTime uint64
}

func readProcessStat(pid int) (processStat, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return processStat{}, err
	}
	return parseProcessStat(payload)
}

func parseProcessStat(payload []byte) (processStat, error) {
	closing := bytes.LastIndex(payload, []byte(") "))
	if closing < 0 {
		return processStat{}, fmt.Errorf("process stat has invalid shape")
	}
	fields := strings.Fields(string(payload[closing+2:]))
	if len(fields) <= 19 {
		return processStat{}, fmt.Errorf("process stat is truncated")
	}
	parentPID, err := strconv.Atoi(fields[1])
	if err != nil {
		return processStat{}, fmt.Errorf("process stat has invalid parent pid: %w", err)
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return processStat{}, fmt.Errorf("process stat has invalid start time: %w", err)
	}
	return processStat{ParentPID: parentPID, StartTime: startTime}, nil
}

func processStartTime(pid int) (uint64, error) {
	stat, err := readProcessStat(pid)
	return stat.StartTime, err
}

func processRootedAt(pid, rootPID int, rootStartTime uint64) (bool, error) {
	if pid <= 1 || rootPID <= 1 || rootStartTime == 0 {
		return false, fmt.Errorf("process ancestry requires non-system process identities")
	}
	visited := make(map[int]struct{}, 16)
	for depth := 0; depth < 64; depth++ {
		if pid <= 1 {
			return false, nil
		}
		if _, duplicate := visited[pid]; duplicate {
			return false, fmt.Errorf("process ancestry contains a cycle")
		}
		visited[pid] = struct{}{}
		stat, err := readProcessStat(pid)
		if err != nil {
			return false, err
		}
		if pid == rootPID {
			return stat.StartTime == rootStartTime, nil
		}
		if stat.ParentPID == pid {
			return false, fmt.Errorf("process ancestry contains a self-parent")
		}
		pid = stat.ParentPID
	}
	return false, fmt.Errorf("process ancestry exceeds 64 generations")
}

func namespacesDistinct(pid int, namespace string) bool {
	child, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "ns", namespace))
	if err != nil {
		return false
	}
	parent, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(os.Getpid()), "ns", namespace))
	return err == nil && child != parent
}

func windowsMountsAbsent(pid int) bool {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "mountinfo"))
	if err != nil {
		return false
	}
	text := strings.ToLower(string(payload))
	return !strings.Contains(text, " drvfs ") && !strings.Contains(text, " 9p ") && !strings.Contains(text, " /mnt/c ")
}

func relayUnix(left, right *net.UnixConn) error {
	var wait sync.WaitGroup
	errorsOut := make(chan error, 2)
	copyOne := func(destination, source *net.UnixConn) {
		defer wait.Done()
		_, err := io.Copy(destination, source)
		_ = destination.CloseWrite()
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

type socketIdentity struct {
	Device uint64
	Inode  uint64
	Owner  uint32
}

type socketOwnership struct {
	Filesystem  socketIdentity
	KernelInode uint64
}

func socketOwnershipChangedDuringAcceptanceError(expected, observed socketOwnership) error {
	return fmt.Errorf(
		"managed Product app-server socket ownership changed during acceptance: expected filesystem device=%d inode=%d owner=%d kernel_inode=%d, observed filesystem device=%d inode=%d owner=%d kernel_inode=%d",
		expected.Filesystem.Device,
		expected.Filesystem.Inode,
		expected.Filesystem.Owner,
		expected.KernelInode,
		observed.Filesystem.Device,
		observed.Filesystem.Inode,
		observed.Filesystem.Owner,
		observed.KernelInode,
	)
}

func proveProcessOwnsListeningUnixSocket(
	pid int,
	expectedStartTime uint64,
	hostPath string,
	processPath string,
) (socketOwnership, error) {
	first, err := readProcessSocketOwnership(pid, expectedStartTime, hostPath, processPath)
	if err != nil {
		return socketOwnership{}, err
	}
	second, err := readProcessSocketOwnership(pid, expectedStartTime, hostPath, processPath)
	if err != nil {
		return socketOwnership{}, err
	}
	if first != second {
		return socketOwnership{}, fmt.Errorf("Product app-server socket ownership changed during proof")
	}
	return first, nil
}

func readProcessSocketOwnership(
	pid int,
	expectedStartTime uint64,
	hostPath string,
	processPath string,
) (socketOwnership, error) {
	startTime, err := processStartTime(pid)
	if err != nil {
		return socketOwnership{}, err
	}
	if startTime != expectedStartTime {
		return socketOwnership{}, fmt.Errorf("Product app-server process generation changed")
	}
	filesystem, err := pathSocketIdentity(hostPath)
	if err != nil {
		return socketOwnership{}, err
	}
	fdInodes, err := processSocketFDInodes(pid)
	if err != nil {
		return socketOwnership{}, err
	}
	listeners, err := processUnixListeners(pid, processPath)
	if err != nil {
		return socketOwnership{}, err
	}
	if len(listeners) == 0 {
		return socketOwnership{}, errSocketNotReady
	}
	if len(listeners) != 1 || !fdInodes[listeners[0]] {
		return socketOwnership{}, fmt.Errorf("pinned Product Codex PID does not own the sole declared listening Unix socket")
	}
	afterStartTime, err := processStartTime(pid)
	if err != nil || afterStartTime != expectedStartTime {
		return socketOwnership{}, fmt.Errorf("Product app-server process generation changed during socket proof: %w", err)
	}
	afterFilesystem, err := pathSocketIdentity(hostPath)
	if err != nil || afterFilesystem != filesystem {
		return socketOwnership{}, fmt.Errorf("Product app-server filesystem socket changed during ownership proof: %w", err)
	}
	return socketOwnership{Filesystem: filesystem, KernelInode: listeners[0]}, nil
}

func processSocketFDInodes(pid int) (map[uint64]bool, error) {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return nil, fmt.Errorf("read Product app-server descriptors: %w", err)
	}
	result := make(map[uint64]bool)
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if err != nil || !strings.HasPrefix(link, "socket:[") || !strings.HasSuffix(link, "]") {
			continue
		}
		inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64)
		if err == nil && inode != 0 {
			result[inode] = true
		}
	}
	return result, nil
}

func processUnixListeners(pid int, exactPath string) ([]uint64, error) {
	payload, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "net", "unix"))
	if err != nil {
		return nil, fmt.Errorf("read Product app-server Unix socket table: %w", err)
	}
	var listeners []uint64
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[7] != exactPath {
			continue
		}
		if fields[3] != "00010000" || fields[4] != "0001" || fields[5] != "01" {
			return nil, fmt.Errorf("declared Product app-server socket is not one listening stream")
		}
		inode, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil || inode == 0 {
			return nil, fmt.Errorf("declared Product app-server socket has invalid kernel inode")
		}
		listeners = append(listeners, inode)
	}
	return listeners, nil
}

func pathSocketIdentity(path string) (socketIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return socketIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 {
		return socketIdentity{}, fmt.Errorf("path is not an exact Unix socket")
	}
	return socketIdentity{Device: uint64(stat.Dev), Inode: stat.Ino, Owner: stat.Uid}, nil
}

func removeExactSocket(path string, identity socketIdentity) error {
	current, err := pathSocketIdentity(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != identity {
		return fmt.Errorf("refusing to remove a replacement Product socket")
	}
	return os.Remove(path)
}
