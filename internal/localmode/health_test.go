package localmode

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestStartTearsDownOnHealthTimeout(t *testing.T) {
	setLocalDevTestHome(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("false\n"), nil
			case commandIs(command, "docker", "version"):
				return nil, nil
			case command.Name == "docker" && slices.Contains(command.Args, "up"):
				return nil, nil
			case commandIs(command, "docker", "logs", "--tail", "200", serverContainerName):
				return []byte("server log\n"), nil
			case commandIsComposeDown(command, false):
				return nil, nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	service := NewService(
		cliruntime.Deps{HTTPClient: server.Client()},
		WithDockerRunner(runner),
		WithHealthURL(server.URL),
		WithTiming(3*time.Millisecond, time.Millisecond, time.Millisecond),
		WithTempDir(t.TempDir()),
	)

	err := service.Start(context.Background(), &out)

	require.ErrorContains(t, err, "server failed to start")
	assert.Contains(t, out.String(), "server log")
	assert.True(t, runner.called("docker", "logs", "--tail", "200", serverContainerName))
	assert.True(t, runner.calledComposeDown(false))
}

func TestCheckHealthSetsRequestDeadline(t *testing.T) {
	service := NewService(cliruntime.Deps{HTTPClient: deadlineCheckingHTTPClient{}})

	err := service.checkHealth(context.Background())

	require.ErrorContains(t, err, "not ready")
}

type deadlineCheckingHTTPClient struct{}

func (deadlineCheckingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if _, ok := req.Context().Deadline(); !ok {
		return nil, errors.New("missing request deadline")
	}
	return nil, errors.New("not ready")
}
