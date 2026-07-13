package localmode

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const localModeProjectID = "22222222-2222-4222-8222-222222222222"

type fakeCommandRunner struct {
	calls []Command
	run   func(context.Context, Command) ([]byte, error)
}

func (f *fakeCommandRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	command.Args = append([]string{}, command.Args...)
	command.Env = append([]string{}, command.Env...)
	f.calls = append(f.calls, command)
	if f.run == nil {
		return nil, nil
	}
	return f.run(ctx, command)
}

func (f *fakeCommandRunner) called(name string, args ...string) bool {
	for _, call := range f.calls {
		if commandIs(call, name, args...) {
			return true
		}
	}
	return false
}

func (f *fakeCommandRunner) calledWithArg(name, arg string) bool {
	for _, call := range f.calls {
		if call.Name == name && slices.Contains(call.Args, arg) {
			return true
		}
	}
	return false
}

func (f *fakeCommandRunner) calledComposeDown(clean bool) bool {
	for _, call := range f.calls {
		if commandIsComposeDown(call, clean) {
			return true
		}
	}
	return false
}

func runningStopRunner(t *testing.T) *fakeCommandRunner {
	t.Helper()
	return &fakeCommandRunner{
		run: func(_ context.Context, command Command) ([]byte, error) {
			switch {
			case commandIs(command, "docker", "inspect", "--format={{.State.Running}}", serverContainerName):
				return []byte("true\n"), nil
			case commandIsComposeDown(command, false), commandIsComposeDown(command, true):
				return nil, nil
			default:
				t.Fatalf("unexpected command: %s", commandDebug(command))
				return nil, nil
			}
		},
	}
}

func commandIs(command Command, name string, args ...string) bool {
	return command.Name == name && slices.Equal(command.Args, args)
}

func commandIsComposeDown(command Command, clean bool) bool {
	args := []string{"compose", "-f", "", "-f", "", "-p", composeProjectName, "down"}
	if clean {
		args = append(args, "-v")
	}
	if command.Name != dockerCommand || len(command.Args) != len(args) {
		return false
	}
	for _, index := range []int{2, 4} {
		if !strings.HasPrefix(command.Args[index], "docker-compose-") && !strings.Contains(command.Args[index], "/docker-compose-") {
			return false
		}
		args[index] = command.Args[index]
	}
	return slices.Equal(command.Args, args)
}

func commandDebug(command Command) string {
	return strings.TrimSpace(command.Name + " " + strings.Join(command.Args, " "))
}

func localModeInfoJSON(apiURL string) string {
	data, err := json.Marshal(localModeInfo(apiURL))
	if err != nil {
		panic(err)
	}
	return string(data)
}

func localModeInfo(apiURL string) Info {
	return Info{
		APIURL:                         apiURL,
		ProjectID:                      localModeProjectID,
		ProjectName:                    "local-dev",
		UserID:                         "local-user",
		AuthUserID:                     "local-auth-user",
		AuthUserEmail:                  "local@example.com",
		UserToken:                      "local-token",
		AnonKey:                        "local-anon-key",
		ServiceKey:                     "local-service-key",
		DefaultDatabaseName:            "app",
		DefaultDatabaseRegion:          "metadata-region",
		DefaultDatabasePostgresVersion: "17",
		DatabaseURL:                    "postgres://volcano:volcano@localhost:8002/app",
		RedisURL:                       "redis://localhost:6379",
		JWTSecret:                      "jwt-secret",
		EncryptionKey:                  "encryption-key",
		AnonKeySecret:                  "anon-key-secret",
		ServiceKeySecret:               "service-key-secret",
	}
}

func setLocalDevTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func withTempWorkingDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
}

func writeLocalModeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
