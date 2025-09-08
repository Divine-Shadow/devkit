package execx

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "time"
)

type Result struct {
    Code int
    Err  error
}

func Run(name string, args ...string) Result {
    return RunCtx(context.Background(), name, args...)
}

func RunCtx(ctx context.Context, name string, args ...string) Result {
    if os.Getenv("DEVKIT_DEBUG") == "1" {
        fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(append([]string{name}, args...), " "))
    }
    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    err := cmd.Run()
    code := 0
    if err != nil {
        if ee, ok := err.(*exec.ExitError); ok {
            code = ee.ExitCode()
        } else if ctx.Err() == context.DeadlineExceeded {
            code = 124
        } else {
            code = 1
        }
    }
    return Result{Code: code, Err: err}
}

func WithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), d)
}

