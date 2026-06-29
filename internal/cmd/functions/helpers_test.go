package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	functionProjectID = "22222222-2222-4222-8222-222222222222"
	functionID        = "33333333-3333-4333-8333-333333333333"
	otherFunctionID   = "44444444-4444-4444-8444-444444444444"
)

func executeFunctionsCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// syncBuffer is an io.Writer safe for concurrent writes and reads, used to
// capture command output while a follow command streams on another goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// streamFunctionsCommand runs cmd with a cancelable context on its own
// goroutine, returning the captured output and a channel that receives the
// command's error when it exits. Use it for --follow commands, which run until
// the context is canceled.
func streamFunctionsCommand(ctx context.Context, cmd *cobra.Command, args ...string) (*syncBuffer, <-chan error) {
	out := &syncBuffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs(args)
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.ExecuteContext(ctx)
	}()
	return out, errCh
}

func setFunctionCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveFunctionCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   functionProjectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeFunctionCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func functionCommandPayload(id, name string) map[string]any {
	return map[string]any{
		"created_at":       "2026-05-20T00:00:00Z",
		"deployed_regions": []string{"aws-us-east-1"},
		"handler":          "handler",
		"id":               id,
		"invoke_url":       "https://" + id + ".functions.volcano.dev/",
		"is_public":        true,
		"name":             name,
		"project_id":       functionProjectID,
		"runtime":          "nodejs24.x",
		"status":           "active",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}

func functionRuntimeCommandPayload(name, language string, isDefault bool, fileExtensions []string, entrypoint, handler string, dependencyManifests []string) map[string]any {
	return map[string]any{
		"name":     name,
		"language": language,
		"default":  isDefault,
		"deployment": map[string]any{
			"file_extensions":      fileExtensions,
			"entrypoint":           entrypoint,
			"handler":              handler,
			"dependency_manifests": dependencyManifests,
		},
	}
}
