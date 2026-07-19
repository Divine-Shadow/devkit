package brokercmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"devkit/cli/devctl/internal/cmdregistry"
	"devkit/cli/devctl/internal/config"
	runtimebroker "devkit/cli/devctl/internal/runtime/broker"
)

type args struct {
	cfg    runtimebroker.Config
	format string
}

func Register(r *cmdregistry.Registry) {
	r.Register("broker", handle)
}

func handle(ctx *cmdregistry.Context) error {
	if len(ctx.Args) == 0 {
		return fmt.Errorf("Usage: broker start|status|stop [--socket PATH] [--allow-image IMAGE] [--format text|json]")
	}
	parsed, err := parse(ctx)
	if err != nil {
		return err
	}
	var status runtimebroker.Status
	switch ctx.Args[0] {
	case "start":
		status, err = runtimebroker.Start(context.Background(), parsed.cfg, ctx.DryRun)
	case "status":
		status, err = runtimebroker.Inspect(parsed.cfg)
	case "stop":
		status, err = runtimebroker.Stop(parsed.cfg, ctx.DryRun)
	default:
		return fmt.Errorf("unknown broker command %s", ctx.Args[0])
	}
	if err != nil {
		return err
	}
	return printStatus(status, parsed.format)
}

func parse(ctx *cmdregistry.Context) (args, error) {
	overlayCfg, _, err := config.ReadAll(ctx.Paths.OverlayPaths, ctx.Project)
	if err != nil {
		return args{}, err
	}
	cfg := runtimebroker.Config{
		DevkitRoot:    ctx.Paths.Root,
		Socket:        resolveBrokerPath(ctx.Paths.Root, overlayCfg.Broker.Socket),
		Upstream:      strings.TrimSpace(overlayCfg.Broker.Upstream),
		AllowedImages: append([]string{}, overlayCfg.Broker.AllowedImages...),
		LogLevel:      strings.TrimSpace(overlayCfg.Broker.LogLevel),
		Binary:        strings.TrimSpace(os.Getenv("DEVKIT_RUNTIME_BROKER_BINARY")),
	}
	if overlayCfg.Broker.AllowPulls != nil {
		cfg.AllowPulls = *overlayCfg.Broker.AllowPulls
	}
	parsed := args{cfg: cfg, format: "text"}
	cliAllowedImages := []string{}
	for i := 1; i < len(ctx.Args); i++ {
		switch ctx.Args[i] {
		case "--socket":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--socket requires a value")
			}
			parsed.cfg.Socket = resolveBrokerPath(ctx.Paths.Root, ctx.Args[i+1])
			i++
		case "--upstream":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--upstream requires a value")
			}
			parsed.cfg.Upstream = ctx.Args[i+1]
			i++
		case "--allow-image":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--allow-image requires a value")
			}
			cliAllowedImages = append(cliAllowedImages, ctx.Args[i+1])
			i++
		case "--allow-pulls":
			parsed.cfg.AllowPulls = true
		case "--no-allow-pulls":
			parsed.cfg.AllowPulls = false
		case "--state-root":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--state-root requires a value")
			}
			parsed.cfg.StateRoot = ctx.Args[i+1]
			i++
		case "--binary":
			return parsed, fmt.Errorf("--binary is not supported; use the immutable runtime package")
		case "--log-level":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--log-level requires a value")
			}
			parsed.cfg.LogLevel = ctx.Args[i+1]
			i++
		case "--format":
			if i+1 >= len(ctx.Args) {
				return parsed, fmt.Errorf("--format requires a value")
			}
			parsed.format = strings.TrimSpace(ctx.Args[i+1])
			i++
		default:
			return parsed, fmt.Errorf("unknown broker arg %s", ctx.Args[i])
		}
	}
	if len(cliAllowedImages) > 0 {
		parsed.cfg.AllowedImages = cliAllowedImages
	}
	parsed.cfg = runtimebroker.Normalize(parsed.cfg)
	return parsed, nil
}

func resolveBrokerPath(devkitRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(devkitRoot, value))
}

func printStatus(status runtimebroker.Status, format string) error {
	switch format {
	case "", "text":
		fmt.Fprintf(os.Stdout, "running: %t\n", status.Running)
		fmt.Fprintf(os.Stdout, "socket: %s\n", status.Socket)
		fmt.Fprintf(os.Stdout, "socket_exists: %t\n", status.SocketExists)
		fmt.Fprintf(os.Stdout, "state_root: %s\n", status.StateRoot)
		if status.PID > 0 {
			fmt.Fprintf(os.Stdout, "pid: %d\n", status.PID)
		}
		if status.StaleState {
			fmt.Fprintf(os.Stdout, "stale_state: %t\n", status.StaleState)
		}
		if status.Message != "" {
			fmt.Fprintf(os.Stdout, "message: %s\n", status.Message)
		}
		if status.LogPath != "" {
			fmt.Fprintf(os.Stdout, "log: %s\n", status.LogPath)
		}
	case "json":
		data, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, string(data))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}
