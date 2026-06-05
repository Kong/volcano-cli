package localmode

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Command describes an external process invocation.
type Command struct {
	Name string
	Args []string
	Env  []string
}

// DockerRunner runs Docker commands for the local-mode environment.
type DockerRunner interface {
	Run(ctx context.Context, command Command) ([]byte, error)
}

// DockerRunnerFunc adapts a function into a DockerRunner.
type DockerRunnerFunc func(context.Context, Command) ([]byte, error)

// Run executes f.
func (f DockerRunnerFunc) Run(ctx context.Context, command Command) ([]byte, error) {
	return f(ctx, command)
}

type execDockerRunner struct{}

type runtimeCommandRunner struct {
	runner cliruntime.CommandRunner
}

func newDockerRunner(deps cliruntime.Deps) DockerRunner {
	if deps.LocalCommandRunner != nil {
		return runtimeCommandRunner{runner: deps.LocalCommandRunner}
	}
	return execDockerRunner{}
}

func (execDockerRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	if command.Name != dockerCommand {
		return nil, fmt.Errorf("unsupported local development command %q", command.Name)
	}

	cmd := exec.CommandContext(ctx, command.Name, command.Args...) //nolint:gosec // local-mode commands provide the command name and args.
	if len(command.Env) > 0 {
		cmd.Env = command.Env
	}

	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}

	if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
		return output, fmt.Errorf("%s: %w", trimmed, err)
	}
	return output, err
}

func (r runtimeCommandRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	if r.runner == nil {
		return nil, errors.New("local command runner is nil")
	}
	return r.runner.Run(ctx, command.Name, command.Args...)
}
