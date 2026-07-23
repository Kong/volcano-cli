package localmode

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestRestartStopsBeforeStarting(t *testing.T) {
	setLocalDevTestHome(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localModeProjectID+"/databases":
			writeLocalModeJSON(t, w, http.StatusConflict, map[string]any{"message": "exists"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	running := true
	var order []string
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				if running {
					return []byte("true\n"), nil
				}
				return []byte("false\n"), nil
			case commandIsComposeDown(command, false):
				order = append(order, "down")
				running = false
				return nil, nil
			case commandIs(command, "docker", "version"):
				return nil, nil
			case command.Name == "docker" && slices.Contains(command.Args, "pull"):
				return nil, nil
			case command.Name == "docker" && slices.Contains(command.Args, "up"):
				order = append(order, "up")
				running = true
				return nil, nil
			case commandIs(command, "docker", "exec", serverContainerName, "/app/volcano-hosting", "local", "info", "--format", "json"):
				return []byte(localModeInfoJSON(server.URL)), nil
			case commandIs(command, "docker", "exec", redisContainerName, "redis-cli", "ping"):
				return []byte("PONG\n"), nil
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
		WithDialTCP(func(context.Context, string) error { return nil }),
		WithTempDir(t.TempDir()),
	)

	require.NoError(t, service.Restart(context.Background(), &out))
	assert.Equal(t, []string{"down", "up"}, order)
}

func TestRestartFailsBeforeTeardownWhenCustomImageMissing(t *testing.T) {
	setLocalDevTestHome(t)
	withTempWorkingDir(t)

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
			case commandIs(command, "docker", "version"):
				return nil, nil
			case commandIs(command, "docker", "image", "inspect", "kong/volcano:local-dev"):
				return nil, errors.New("Error: No such image: kong/volcano:local-dev")
			case commandIsComposeDown(command, false):
				t.Fatalf("compose down must not run when the custom image is missing")
				return nil, nil
			case command.Name == "docker" && slices.Contains(command.Args, "pull"):
				return nil, nil
			case command.Name == "docker" && slices.Contains(command.Args, "up"):
				t.Fatalf("compose up must not run when the custom image is missing")
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	service := NewService(
		cliruntime.Deps{},
		WithDockerRunner(runner),
		WithImage("kong/volcano:local-dev"),
		WithEnvironment(func() []string { return []string{"PATH=/bin"} }, func(string) string { return "" }),
		WithTempDir(t.TempDir()),
	)

	err := service.Restart(context.Background(), &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found locally")
	assert.False(t, runner.calledWithArg("docker", "down"), "environment must not be torn down on a bad --image")
}
