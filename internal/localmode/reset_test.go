package localmode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestResetInvokesServerEntrypointAndRefreshesDevState(t *testing.T) {
	setLocalDevTestHome(t)

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "version"):
				return []byte("Docker version 1\n"), nil
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
			case commandIs(command, "docker", localResetCommandArgs()...):
				return []byte("Local reset complete.\nDefault database: app\nDropped databases: [project_a]\n"), nil
			case commandIs(command, "docker", "exec", serverContainerName, serverBinaryPath, "local", "info", "--format", "json"):
				return []byte(localModeInfoJSON("http://localhost:8000")), nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Reset(context.Background(), &out)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Local reset complete.")
	assert.Contains(t, out.String(), "Default database: app")
	assert.Contains(t, out.String(), "Dropped databases: [project_a]")
	assert.Contains(t, out.String(), "volcano local migrations deploy --all -d app")
	assert.True(t, runner.called("docker", localResetCommandArgs()...))

	statePath, err := DevStatePath()
	require.NoError(t, err)
	stateData, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(stateData), `"project_id": "`+localModeProjectID+`"`)
	assert.Contains(t, string(stateData), `"user_token": "local-token"`)
}

func TestResetRequiresDocker(t *testing.T) {
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "version"):
				return nil, errors.New("docker missing")
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Reset(context.Background(), &out)

	require.ErrorContains(t, err, "docker not found")
	assert.Contains(t, out.String(), "Docker is not available")
	assert.False(t, runner.called("docker", localResetCommandArgs()...))
}

func TestResetRequiresRunningServer(t *testing.T) {
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "version"):
				return []byte("Docker version 1\n"), nil
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("false\n"), nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Reset(context.Background(), &out)

	require.ErrorContains(t, err, "run 'volcano start' first")
	assert.False(t, runner.called("docker", localResetCommandArgs()...))
}

func TestResetSurfacesServerOutput(t *testing.T) {
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "version"):
				return []byte("Docker version 1\n"), nil
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
			case commandIs(command, "docker", localResetCommandArgs()...):
				return []byte("server-side failure details"), errors.New("exit status 1")
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Reset(context.Background(), &out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server-side failure details")
	assert.NotContains(t, out.String(), "Local reset complete.")
}

func TestLocalResetCommandArgsReturnsIndependentSlice(t *testing.T) {
	first := localResetCommandArgs()
	second := localResetCommandArgs()
	first[0] = "mutated"

	require.False(t, reflect.DeepEqual(first, second))
	require.Equal(t, "exec", second[0])
}
