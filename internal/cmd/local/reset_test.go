package local

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestLocalResetCommandInvokesServerEntrypoint(t *testing.T) {
	setLocalCommandTestEnv(t)

	var commands [][]string
	deps := cliruntime.Deps{
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			require.Equal(t, "docker", name)
			commands = append(commands, append([]string{}, args...))
			switch {
			case slices.Equal(args, []string{"version"}):
				return []byte("Docker version 1\n"), nil
			case slices.Equal(args, []string{"inspect", "--format={{.State.Running}}", "volcano-server"}):
				return []byte("true\n"), nil
			case slices.Equal(args, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "reset", "--yes", "--format", "text"}):
				return []byte("Local reset complete.\nDefault database: app\n"), nil
			case slices.Equal(args, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}):
				return []byte(localInfoJSON("http://localhost:8000")), nil
			default:
				t.Fatalf("unexpected command args: %v", args)
				return nil, nil
			}
		}),
	}

	out, err := executeLocalCommand(t, New(deps), "reset", "--yes")

	require.NoError(t, err)
	assert.Contains(t, out, "Local reset complete.")
	assert.Contains(t, out, "volcano migrations deploy --all -d app")
	assert.True(t, slices.ContainsFunc(commands, func(command []string) bool {
		return slices.Equal(command, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "reset", "--yes", "--format", "text"})
	}))
}

func TestLocalResetCommandCanBeCancelled(t *testing.T) {
	setLocalCommandTestEnv(t)

	deps := cliruntime.Deps{
		LocalCommandRunner: localmode.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("reset should not run")
		}),
	}

	out, err := executeLocalCommandWithInput(t, New(deps), "no\n", "reset")

	require.NoError(t, err)
	assert.Contains(t, out, "WARNING")
	assert.Contains(t, out, "Reset cancelled.")
}

func TestLocalResetCommandPromptsAndRuns(t *testing.T) {
	setLocalCommandTestEnv(t)

	var resetCalled bool
	deps := cliruntime.Deps{
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch {
			case slices.Equal(args, []string{"version"}):
				return []byte("Docker version 1\n"), nil
			case slices.Equal(args, []string{"inspect", "--format={{.State.Running}}", "volcano-server"}):
				return []byte("true\n"), nil
			case slices.Equal(args, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "reset", "--yes", "--format", "text"}):
				resetCalled = true
				return []byte("Local reset complete.\n"), nil
			case slices.Equal(args, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}):
				return []byte(localInfoJSON("http://localhost:8000")), nil
			default:
				t.Fatalf("unexpected command args: %v", args)
				return nil, nil
			}
		}),
	}

	out, err := executeLocalCommandWithInput(t, New(deps), "yes\n", "reset")

	require.NoError(t, err)
	assert.True(t, resetCalled)
	assert.Contains(t, out, "Local reset complete.")
}

func executeLocalCommandWithInput(t *testing.T, cmd *cobra.Command, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetIn(bytes.NewBufferString(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
