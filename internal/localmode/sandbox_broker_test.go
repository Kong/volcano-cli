package localmode

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestSandboxBrokerFailureDoesNotBlockCoreServices(t *testing.T) {
	withTempWorkingDir(t)
	var starts []string
	runner := &fakeCommandRunner{run: func(_ context.Context, command Command) ([]byte, error) {
		if slices.Contains(command.Args, "up") {
			if slices.Contains(command.Args, "sandbox-broker") {
				starts = append(starts, "broker")
				assert.NotContains(t, command.Args, "--wait")
				return nil, errors.New("Docker socket unavailable")
			}
			starts = append(starts, "core")
		}
		return nil, nil
	}}
	service := NewService(cliruntime.Deps{}, WithDockerRunner(runner), WithTempDir(t.TempDir()))
	var out bytes.Buffer
	require.NoError(t, service.startDockerServices(t.Context(), &out, nil))
	assert.Equal(t, []string{"core", "broker"}, starts)
	assert.Contains(t, out.String(), "Sandbox broker could not start")
	assert.Contains(t, out.String(), "other local services remain available")
}

func TestCoreStartupFailureDoesNotStartSandboxBroker(t *testing.T) {
	withTempWorkingDir(t)
	failure := errors.New("core startup failed")
	runner := &fakeCommandRunner{run: func(_ context.Context, command Command) ([]byte, error) {
		assert.NotContains(t, command.Args, "sandbox-broker")
		if slices.Contains(command.Args, "up") {
			return nil, failure
		}
		return nil, nil
	}}
	service := NewService(cliruntime.Deps{}, WithDockerRunner(runner), WithTempDir(t.TempDir()))
	require.ErrorIs(t, service.startDockerServices(t.Context(), &bytes.Buffer{}, nil), failure)
}

func TestComposeDownIncludesSandboxBrokerAndPreservesFailure(t *testing.T) {
	t.Parallel()
	for _, clean := range []bool{false, true} {
		t.Run(map[bool]string{false: "stop", true: "clean"}[clean], func(t *testing.T) {
			t.Parallel()
			failure := errors.New("broker cleanup failed")
			runner := &fakeCommandRunner{run: func(_ context.Context, command Command) ([]byte, error) {
				assert.True(t, commandIsComposeDown(command, clean), commandDebug(command))
				return nil, failure
			}}
			service := NewService(cliruntime.Deps{}, WithDockerRunner(runner), WithTempDir(t.TempDir()))
			require.ErrorIs(t, service.composeDown(t.Context(), clean), failure)
		})
	}
}
