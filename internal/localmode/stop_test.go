package localmode

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestStopCleanControlsVolumesAndDevState(t *testing.T) {
	setLocalDevTestHome(t)
	require.NoError(t, saveDevState(localModeInfo("http://localhost:8000")))
	statePath, err := DevStatePath()
	require.NoError(t, err)

	nonCleanRunner := runningStopRunner(t)
	var out bytes.Buffer
	err = NewService(cliruntime.Deps{}, WithDockerRunner(nonCleanRunner)).Stop(context.Background(), &out, false)
	require.NoError(t, err)
	assert.True(t, nonCleanRunner.calledComposeDown(false))
	assert.False(t, nonCleanRunner.calledComposeDown(true))
	_, err = os.Stat(statePath)
	require.NoError(t, err)

	cleanRunner := runningStopRunner(t)
	out.Reset()
	err = NewService(cliruntime.Deps{}, WithDockerRunner(cleanRunner)).Stop(context.Background(), &out, true)
	require.NoError(t, err)
	assert.True(t, cleanRunner.calledComposeDown(true))
	_, err = os.Stat(statePath)
	require.True(t, os.IsNotExist(err), "expected dev state to be deleted, got %v", err)
}

func TestStopCleanRunsWhenServerIsStopped(t *testing.T) {
	setLocalDevTestHome(t)
	require.NoError(t, saveDevState(localModeInfo("http://localhost:8000")))
	statePath, err := DevStatePath()
	require.NoError(t, err)

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("false\n"), nil
			case commandIsComposeDown(command, true):
				return nil, nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err = NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Stop(context.Background(), &out, true)
	require.NoError(t, err)

	assert.True(t, runner.calledComposeDown(true))
	_, err = os.Stat(statePath)
	require.True(t, os.IsNotExist(err), "expected dev state to be deleted, got %v", err)
}

func TestStopRunsWhenComposeContainersAreStopped(t *testing.T) {
	setLocalDevTestHome(t)
	require.NoError(t, saveDevState(localModeInfo("http://localhost:8000")))
	statePath, err := DevStatePath()
	require.NoError(t, err)

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("false\n"), nil
			case commandIs(command, "docker", "ps", "-a", "--quiet", "--filter", "label=com.docker.compose.project="+composeProjectName):
				return []byte("abc123\n"), nil
			case commandIsComposeDown(command, false):
				return nil, nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err = NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Stop(context.Background(), &out, false)
	require.NoError(t, err)

	assert.True(t, runner.calledComposeDown(false))
	_, err = os.Stat(statePath)
	require.NoError(t, err)
}
