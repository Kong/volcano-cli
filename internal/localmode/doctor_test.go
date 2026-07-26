package localmode

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func doctorRunner(client, server, compose, running []byte, clientErr, serverErr, composeErr error) *fakeCommandRunner {
	return &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "--version"):
				return client, clientErr
			case commandIs(command, "docker", "version", "--format", "{{.Server.Version}}"):
				return server, serverErr
			case commandIs(command, "docker", "compose", "version", "--short"):
				return compose, composeErr
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return running, nil
			default:
				return nil, errors.New("unexpected command: " + commandDebug(command))
			}
		},
	}
}

func TestParseDockerVersionLine(t *testing.T) {
	assert.Equal(t, "28.4.0", parseDockerVersionLine("Docker version 28.4.0, build d8eb465f86\n"))
	assert.Equal(t, "weird output", parseDockerVersionLine("  weird output  "))
}

func TestDoctorAllPass(t *testing.T) {
	runner := doctorRunner([]byte("Docker version 27.0.3, build abc123\n"), []byte("27.0.3\n"), []byte("v2.28.1\n"), []byte("false\n"), nil, nil, nil)

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Doctor(context.Background(), &out)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "✓ Docker CLI (27.0.3)")
	assert.Contains(t, out.String(), "✓ Docker engine (27.0.3)")
	assert.Contains(t, out.String(), "✓ Docker Compose v2 (v2.28.1)")
	assert.Contains(t, out.String(), "All checks passed. Run 'volcano start'")
}

func TestDoctorReportsRunningStack(t *testing.T) {
	runner := doctorRunner([]byte("Docker version 27.0.3, build abc123"), []byte("27.0.3"), []byte("v2.28.1"), []byte("true\n"), nil, nil, nil)

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Doctor(context.Background(), &out)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "local development is running")
}

func TestDoctorDockerCLIMissing(t *testing.T) {
	runner := doctorRunner(nil, nil, nil, nil, errors.New(`exec: "docker": executable file not found in $PATH`), nil, nil)

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Doctor(context.Background(), &out)

	require.Error(t, err)
	assert.Contains(t, out.String(), "✗ Docker CLI")
	assert.Contains(t, out.String(), "Docker-compatible engine")
	// Fails fast: does not probe engine/compose once the CLI is absent.
	assert.False(t, runner.called("docker", "version", "--format", "{{.Server.Version}}"))
	assert.True(t, runner.called("docker", "--version"))
}

func TestDoctorEngineNotRunning(t *testing.T) {
	// Daemon down: `docker --version` (client probe) exits 0, but the engine
	// probe fails. This is the realistic installed-but-daemon-down state.
	runner := doctorRunner([]byte("Docker version 27.0.3, build abc123"), nil, []byte("v2.28.1"), nil, nil, errors.New("Cannot connect to the Docker daemon"), nil)

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Doctor(context.Background(), &out)

	require.Error(t, err)
	assert.Contains(t, out.String(), "✓ Docker CLI")
	assert.Contains(t, out.String(), "✗ Docker engine")
	assert.Contains(t, out.String(), "isn't reachable")
	// Compose is still probed so the report is complete.
	assert.Contains(t, out.String(), "✓ Docker Compose v2")
}

func TestDoctorComposeMissing(t *testing.T) {
	runner := doctorRunner([]byte("Docker version 27.0.3, build abc123"), []byte("27.0.3"), nil, nil, nil, nil, errors.New("docker: 'compose' is not a docker command"))

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Doctor(context.Background(), &out)

	require.Error(t, err)
	assert.Contains(t, out.String(), "✗ Docker Compose v2")
	assert.Contains(t, out.String(), "docker compose")
}
