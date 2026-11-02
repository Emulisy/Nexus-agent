package tool

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// SandboxConfig selects the execution image and maximum container runtime.
// Memory, CPU, PID, and network settings are fixed in Run.
type SandboxConfig struct {
	Image   string
	Timeout time.Duration
}

// Sandbox creates a disposable Docker container for each code execution. No
// network is attached, and fixed memory/CPU/PID limits bound the container. The
// client honors Docker environment settings rather than assuming a local socket.
type Sandbox struct {
	cli *client.Client
	cfg SandboxConfig
}

// NewSandbox creates a Docker client using environment configuration and API
// version negotiation. Images and daemon availability are checked by later calls.
func NewSandbox(cfg SandboxConfig) (*Sandbox, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("new docker client: %w", err)
	}
	return &Sandbox{cli: cli, cfg: cfg}, nil
}

// ExecResult contains separated standard output/error, the inspected exit code,
// and elapsed wall-clock duration. Output is truncated before leaving the sandbox.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}

// Run selects a supported interpreter, creates and starts one constrained
// container, waits for exit under a timeout, and collects logs. Deferred removal
// uses a fresh cleanup context so cancellation does not leave the container behind.
func (s *Sandbox) Run(ctx context.Context, language, code string) (*ExecResult, error) {
	start := time.Now()

	var cmd []string
	switch language {
	case "python":
		cmd = []string{"python3", "-c", code}
	case "shell":
		cmd = []string{"sh", "-c", code}
	// The Go input must be a complete main package. Code is written inside the
	// container; no workspace directory is mounted into the execution environment.
	case "go":
		cmd = []string{"sh", "-c", fmt.Sprintf("mkdir -p /tmp/x && cat > /tmp/x/main.go <<'NEXUSEOF'\n%s\nNEXUSEOF\ncd /tmp/x && go run main.go", code)}
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	// Bound create/start/wait/log operations with the configured execution deadline.
	runCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	resp, err := s.cli.ContainerCreate(runCtx,
		&container.Config{
			Image:      s.cfg.Image,
			Cmd:        cmd,
			WorkingDir: "/sandbox",
			Tty:        false,
			Env:        []string{"GOCACHE=/tmp/gocache", "GOFLAGS=-mod=mod"},
		},
		&container.HostConfig{
			// Keep runtime code off the network; dependency downloads are unavailable.
			NetworkMode: "none",
			Resources: container.Resources{
				Memory:    512 * 1024 * 1024,
				NanoCPUs:  1e9,
				PidsLimit: int64Ptr(64),
			},
		},
		nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("container create: %w", err)
	}
	// Once a container exists, schedule forced removal for success, error, and timeout.
	containerID := resp.ID

	defer func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer rmCancel()
		_ = s.cli.ContainerRemove(rmCtx, containerID, container.RemoveOptions{Force: true})
	}()

	if err := s.cli.ContainerStart(runCtx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("container start: %w", err)
	}

	statusCh, errCh := s.cli.ContainerWait(runCtx, containerID, container.WaitConditionNotRunning)
	select {
	case <-statusCh:
	case err := <-errCh:
		return nil, fmt.Errorf("container wait: %w", err)
	case <-runCtx.Done():
		return nil, fmt.Errorf("sandbox timeout (%s)", s.cfg.Timeout)
	}

	logs, err := s.cli.ContainerLogs(runCtx, containerID, container.LogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: false,
	})
	if err != nil {
		return nil, fmt.Errorf("container logs: %w", err)
	}
	defer logs.Close()

	var stdout, stderr bytes.Buffer

	// Docker multiplexes non-TTY logs; demultiplex before reporting stdout/stderr.
	_, _ = stdcopy.StdCopy(&stdout, &stderr, logs)

	inspect, err := s.cli.ContainerInspect(context.Background(), containerID)
	exitCode := -1
	if err == nil && inspect.State != nil {
		exitCode = inspect.State.ExitCode
	}

	return &ExecResult{
		Stdout:   truncate(strings.TrimSpace(stdout.String()), 64*1024),
		Stderr:   truncate(strings.TrimSpace(stderr.String()), 64*1024),
		ExitCode: exitCode,
		Duration: time.Since(start),
	}, nil
}

func int64Ptr(v int64) *int64 { return &v }

// truncate limits stored/output bytes and marks omitted content.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}
