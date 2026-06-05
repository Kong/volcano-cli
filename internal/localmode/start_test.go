package localmode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestStartChecksDockerAvailability(t *testing.T) {
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return nil, errors.New("container not found")
			case commandIs(command, "docker", "version"):
				return nil, errors.New("docker missing")
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}

	var out bytes.Buffer
	err := NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Start(context.Background(), &out)

	require.ErrorContains(t, err, "docker not found")
	assert.Contains(t, out.String(), "Docker is not available")
	assert.True(t, runner.called("docker", "version"))
}

func TestStartCreatesStackPersistsMetadataAndDefaultDatabase(t *testing.T) {
	setLocalDevTestHome(t)
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte("VOLCANO_IMAGE=kong/volcano:from-file\nVOLCANO_LOG_LEVEL=debug\nQUOTED=\"value\"\nINLINE=kept # comment\n"), 0o600))

	var createBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localModeProjectID+"/databases":
			assert.Equal(t, "Bearer local-token", r.Header.Get("Authorization"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			createBodies = append(createBodies, body)
			writeLocalModeJSON(t, w, http.StatusCreated, map[string]any{"name": "app"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	started := false
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				if started {
					return []byte("true\n"), nil
				}
				return []byte("false\n"), nil
			case commandIs(command, "docker", "version"):
				return []byte("Docker version 1\n"), nil
			case command.Name == "docker" && slices.Contains(command.Args, "up"):
				started = true
				image, ok := lastEnvValue(command.Env, "VOLCANO_IMAGE")
				require.True(t, ok)
				assert.Equal(t, "kong/volcano:nightly", image)
				assert.Contains(t, command.Env, "VOLCANO_LOG_LEVEL=debug")
				assert.Contains(t, command.Env, "QUOTED=value")
				assert.Contains(t, command.Env, "INLINE=kept")
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
		WithEnvironment(func() []string { return []string{"PATH=/bin"} }, func(key string) string {
			if key == "VOLCANO_IMAGE" {
				return "kong/volcano:nightly"
			}
			return ""
		}),
		WithTempDir(t.TempDir()),
	)

	require.NoError(t, service.Start(context.Background(), &out))

	require.Len(t, createBodies, 1)
	assert.Equal(t, map[string]any{
		"name":       "app",
		"region":     "metadata-region",
		"pg_version": "17",
	}, createBodies[0])
	assert.Contains(t, out.String(), "Volcano is ready for local development.")
	assert.True(t, runner.calledWithArg("docker", "compose"))

	statePath, err := DevStatePath()
	require.NoError(t, err)
	stateData, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(stateData), `"project_id": "`+localModeProjectID+`"`)
	assert.Contains(t, string(stateData), `"user_token": "local-token"`)
}

func TestStartRetriesSetupWhenServerAlreadyRunning(t *testing.T) {
	setLocalDevTestHome(t)

	var createBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localModeProjectID+"/databases":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			createBodies = append(createBodies, body)
			writeLocalModeJSON(t, w, http.StatusCreated, map[string]any{"name": "app"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
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
	)

	require.NoError(t, service.Start(context.Background(), &out))

	require.Len(t, createBodies, 1)
	assert.Equal(t, map[string]any{
		"name":       "app",
		"region":     "metadata-region",
		"pg_version": "17",
	}, createBodies[0])
	assert.False(t, runner.called("docker", "version"))
	assert.False(t, runner.calledWithArg("docker", "compose"))
	assert.Contains(t, out.String(), "Volcano is already running.")
	assert.Contains(t, out.String(), "Default database 'app' ready")
	assert.Contains(t, out.String(), "Dev state saved")

	statePath, err := DevStatePath()
	require.NoError(t, err)
	stateData, err := os.ReadFile(statePath)
	require.NoError(t, err)
	assert.Contains(t, string(stateData), `"project_id": "`+localModeProjectID+`"`)
}

func TestStartWaitsForHealthBeforeReusingRunningServer(t *testing.T) {
	setLocalDevTestHome(t)

	var healthChecks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			check := healthChecks.Add(1)
			if check < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localModeProjectID+"/databases":
			writeLocalModeJSON(t, w, http.StatusCreated, map[string]any{"name": "app"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
			case commandIs(command, "docker", "exec", serverContainerName, "/app/volcano-hosting", "local", "info", "--format", "json"):
				require.GreaterOrEqual(t, healthChecks.Load(), int32(3))
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
		WithTiming(50*time.Millisecond, time.Millisecond, time.Millisecond),
		WithDialTCP(func(context.Context, string) error { return nil }),
	)

	require.NoError(t, service.Start(context.Background(), &out))

	assert.Contains(t, out.String(), "Waiting for server..")
	assert.Contains(t, out.String(), "Server is ready")
	assert.GreaterOrEqual(t, healthChecks.Load(), int32(3))
}

func TestStartFailsWhenDefaultDatabaseCreationFails(t *testing.T) {
	setLocalDevTestHome(t)
	withTempWorkingDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localModeProjectID+"/databases":
			writeLocalModeJSON(t, w, http.StatusInternalServerError, map[string]any{"message": "boom"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	started := false
	runner := &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				if started {
					return []byte("true\n"), nil
				}
				return []byte("false\n"), nil
			case commandIs(command, "docker", "version"):
				return []byte("Docker version 1\n"), nil
			case command.Name == "docker" && slices.Contains(command.Args, "up"):
				started = true
				return nil, nil
			case commandIs(command, "docker", "exec", serverContainerName, "/app/volcano-hosting", "local", "info", "--format", "json"):
				return []byte(localModeInfoJSON(server.URL)), nil
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

	err := service.Start(context.Background(), &out)

	require.ErrorContains(t, err, "failed to create default database")
	require.ErrorContains(t, err, "HTTP 500: boom")
	assert.NotContains(t, out.String(), "Volcano is ready for local development.")
	statePath, err := DevStatePath()
	require.NoError(t, err)
	_, err = os.Stat(statePath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func lastEnvValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, entry := range slices.Backward(env) {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			return value, true
		}
	}
	return "", false
}
