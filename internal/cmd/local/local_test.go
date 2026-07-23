package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const localProjectID = "22222222-2222-4222-8222-222222222222"

func TestLocalDatabaseCommandsUseLocalMetadata(t *testing.T) {
	setLocalCommandTestEnv(t)

	var listQueries []string
	var createBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+localProjectID+"/databases":
			listQueries = append(listQueries, r.URL.RawQuery)
			writeLocalCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{localDatabasePayload("33333333-3333-4333-8333-333333333333", "app")},
				"has_more": false,
				"page":     1,
				"limit":    5,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+localProjectID+"/databases":
			var createBody map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			createBodies = append(createBodies, createBody)
			writeLocalCommandJSON(t, w, http.StatusCreated, localDatabasePayload("44444444-4444-4444-8444-444444444444", "app"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var infoCalls int
	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			infoCalls++
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			remaining := time.Until(deadline)
			assert.Positive(t, remaining)
			assert.LessOrEqual(t, remaining, localInfoTimeout)
			assert.Equal(t, "docker", name)
			assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, args)
			return []byte(localInfoJSON(server.URL)), nil
		}),
	}

	cmd := New(deps)

	out, err := executeLocalCommand(t, cmd, "databases", "list", "--limit", "5")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=5"}, listQueries)
	assert.Contains(t, out, "app")

	out, err = executeLocalCommand(t, cmd, "databases", "create", "app")
	require.NoError(t, err)
	require.Len(t, createBodies, 1)
	assert.Equal(t, map[string]any{
		"name":       "app",
		"region":     "metadata-region",
		"pg_version": "17",
	}, createBodies[0])
	assert.Contains(t, out, "Database 'app' created")
	assert.Equal(t, 1, infoCalls)
}

func TestLocalCommandIgnoresMalformedPersistedConfig(t *testing.T) {
	setLocalCommandTestEnv(t)

	configPath := filepath.Join(os.Getenv("HOME"), ".volcano", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte("{not-json"), 0o600))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+localProjectID+"/databases":
			writeLocalCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{localDatabasePayload("33333333-3333-4333-8333-333333333333", "app")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			assert.Equal(t, "docker", name)
			assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, args)
			return []byte(localInfoJSON(server.URL)), nil
		}),
	}

	out, err := executeLocalCommand(t, New(deps), "databases", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "app")
}

func TestLocalFunctionRuntimesUseLocalMetadata(t *testing.T) {
	setLocalCommandTestEnv(t)

	var cloudHits int
	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloudHits++
		writeLocalCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{localFunctionRuntimePayload("cloud-runtime")},
		})
	}))
	defer cloudServer.Close()
	t.Setenv("VOLCANO_API_URL", cloudServer.URL)

	var localHits int
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/functions/runtimes":
			localHits++
			writeLocalCommandJSON(t, w, http.StatusOK, map[string]any{
				"runtimes": []any{localFunctionRuntimePayload("local-runtime")},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer localServer.Close()

	var infoCalls int
	deps := cliruntime.Deps{
		HTTPClient: localServer.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			infoCalls++
			assert.Equal(t, "docker", name)
			assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, args)
			return []byte(localInfoJSON(localServer.URL)), nil
		}),
	}

	out, err := executeLocalCommand(t, New(deps), "functions", "runtimes")
	require.NoError(t, err)
	assert.Contains(t, out, "local-runtime")
	assert.NotContains(t, out, "cloud-runtime")
	assert.Equal(t, 1, localHits)
	assert.Equal(t, 0, cloudHits)
	assert.Equal(t, 1, infoCalls)
}

func TestLocalStorageObjectCommandsSendNoCredential(t *testing.T) {
	setLocalCommandTestEnv(t)

	projectDir := t.TempDir()
	localPath := filepath.Join(projectDir, "hello.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("hi from local service key"), 0o600))

	var capturedFile string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Local mode sends no credential; the local server defaults to the
		// pre-provisioned local user.
		assert.Empty(t, r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/storage/uploads/greetings/hello.txt":
			contentType := r.Header.Get("Content-Type")
			require.True(t, strings.HasPrefix(contentType, "multipart/form-data"), "expected multipart, got %q", contentType)
			_, params, err := mime.ParseMediaType(contentType)
			require.NoError(t, err)
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				if part.FormName() != "file" {
					continue
				}
				data, err := io.ReadAll(part)
				require.NoError(t, err)
				capturedFile = string(data)
			}
			writeLocalCommandJSON(t, w, http.StatusCreated, map[string]any{
				"id":         "66666666-6666-4666-8666-666666666666",
				"bucket_id":  "44444444-4444-4444-8444-444444444444",
				"name":       "greetings/hello.txt",
				"size":       25,
				"mime_type":  "text/plain",
				"is_public":  false,
				"created_at": "2026-05-20T00:00:00Z",
				"updated_at": "2026-05-20T00:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			assert.Equal(t, "docker", name)
			assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, args)
			return []byte(localInfoJSON(server.URL)), nil
		}),
	}

	out, err := executeLocalCommand(t, New(deps), "storage", "object", "upload", "uploads", localPath, "greetings/hello.txt")
	require.NoError(t, err)
	assert.Contains(t, out, "Uploaded")
	assert.Equal(t, "hi from local service key", capturedFile)
}

func TestLocalCommandSurfacesInfoError(t *testing.T) {
	setLocalCommandTestEnv(t)

	_, err := executeLocalCommand(t, New(cliruntime.Deps{
		LocalCommandRunner: localmode.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New("docker failed")
		}),
	}), "databases", "list")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
	assert.Contains(t, err.Error(), "failed to run local info command")
	assert.Contains(t, err.Error(), "is the volcano-server container running?")
	assert.Contains(t, err.Error(), "docker failed")
}

func TestLocalStorageUsesPluralAliases(t *testing.T) {
	out, err := executeLocalCommand(t, New(cliruntime.Deps{}), "storage", "buckets", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "Aliases:")
	assert.Contains(t, out, "bucket, buckets")

	out, err = executeLocalCommand(t, New(cliruntime.Deps{}), "storage", "policies", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "Aliases:")
	assert.Contains(t, out, "policy, policies")

	out, err = executeLocalCommand(t, New(cliruntime.Deps{}), "storage", "objects", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "Aliases:")
	assert.Contains(t, out, "object, objects")
}

func TestLocalMigrationsUsesDirectDeployPath(t *testing.T) {
	out, err := executeLocalCommand(t, New(cliruntime.Deps{}), "migrations", "deploy", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "volcano migrations deploy")
}

func TestLocalHelpIncludesReset(t *testing.T) {
	out, err := executeLocalCommand(t, New(cliruntime.Deps{}), "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "reset")
}

func TestLocalRejectsUnknownNextJSDeployPath(t *testing.T) {
	_, err := executeLocalCommand(t, New(cliruntime.Deps{}), "nextjs", "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
	assert.Contains(t, err.Error(), "nextjs")
}

func executeLocalCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setLocalCommandTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "cloud-token")
	t.Setenv("VOLCANO_PROJECT_ID", "99999999-9999-4999-8999-999999999999")
	t.Setenv("VOLCANO_API_URL", "https://cloud.example")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func writeLocalCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func localInfoJSON(apiURL string) string {
	return fmt.Sprintf(`{
		"api_url": %q,
		"project_id": %q,
		"project_name": "local-dev",
		"user_id": "local-user",
		"user_token": "local-token",
		"service_key": "local-service-key",
		"default_database_name": "app",
		"default_database_region": "metadata-region",
		"default_database_postgres_version": "17",
		"database_url": "postgres://volcano:volcano@localhost:8002/app"
	}`, apiURL, localProjectID)
}

func localDatabasePayload(id, name string) map[string]any {
	return map[string]any{
		"connection_string": "postgres://example",
		"created_at":        time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"database_type":     "volcano-db-s",
		"id":                id,
		"name":              name,
		"pg_version":        "16",
		"project_id":        localProjectID,
		"region":            "local",
		"status":            "active",
		"updated_at":        time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
	}
}

func localFunctionRuntimePayload(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"language": "nodejs",
		"default":  true,
		"deployment": map[string]any{
			"file_extensions":      []string{".js"},
			"entrypoint":           "index.js",
			"handler":              "handler",
			"dependency_manifests": []string{"package.json"},
		},
	}
}
