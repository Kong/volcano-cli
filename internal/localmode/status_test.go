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

func TestStatusReportsNotRunning(t *testing.T) {
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			if commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName) {
				return nil, errors.New("container not found")
			}
			t.Fatalf("unexpected command: %s", commandDebug(command))
			return nil, nil
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Status(context.Background(), &out)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Status: Not running")
	assert.Contains(t, out.String(), "volcano start")
}
