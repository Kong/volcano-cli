package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const deployProjectID = "88888888-8888-4888-8888-888888888888"

func executeDeployCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setDeployCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: deployProjectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
	t.Chdir(t.TempDir())
}

func writeDeployProjectFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// writeFullProject lays out a project with something for every step except
// migrations.
func writeFullProject(t *testing.T) {
	t.Helper()
	writeDeployProjectFile(t, "volcano/volcano.env", "API_URL=https://api.example.test\n")
	writeDeployProjectFile(t, "volcano/functions/hello.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDeployProjectFile(t, "volcano/volcano-config.yaml", `version: 1
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
frontends:
  - name: web
    variable_scope: scoped
    variables: [API_URL]
    function_routes:
      - function: hello
        path_prefix: /api
`)
	writeDeployProjectFile(t, ".volcano/source-layout.json",
		`{"version": 1, "frontends": [{"name": "web", "workspace": "", "app_root": "web"}]}`)
	writeDeployProjectFile(t, "web/package.json", `{"name": "web", "private": true}`)
}

// fakeVolcano serves the endpoints a deploy uses and records the mutating
// requests in the order they arrive.
type fakeVolcano struct {
	t          *testing.T
	sourceMode string
	// failPath answers 500 to POST or PUT requests for this path suffix.
	failPath string
	// statuses are the deployment statuses the feed reports per resource
	// name, one per poll; the last one repeats. Unlisted names are active.
	statuses map[string][]string
	// unlisted names deploy without their deployment showing in the feed.
	unlisted map[string]bool
	// superseding names resources whose deployment the feed reports as
	// superseded by a newer one, with that one's statuses per poll.
	superseding map[string][]string
	// queuedBehind names resources with a newer deployment queued behind the
	// one this run started, with the newer one's statuses per poll.
	queuedBehind map[string][]string
	// durableStatuses are the statuses a durable function read reports per
	// name, one per read; the last one repeats. Unlisted names are active.
	durableStatuses map[string][]string

	mu        sync.Mutex
	calls     []string
	configs   []map[string]any
	frontends map[string]map[string][]string
	// archives lists the files in each frontend's upload.
	archives    map[string][]string
	deployments map[string]uuid.UUID
	successors  map[string]uuid.UUID
	resources   map[string]uuid.UUID
	polls       int
	reads       []string
	// durableReads counts the reads of each durable function.
	durableReads map[string]int
}

func newFakeVolcano(t *testing.T) *fakeVolcano {
	t.Helper()
	return &fakeVolcano{
		t:            t,
		sourceMode:   "platform",
		frontends:    map[string]map[string][]string{},
		archives:     map[string][]string{},
		deployments:  map[string]uuid.UUID{},
		successors:   map[string]uuid.UUID{},
		resources:    map[string]uuid.UUID{},
		durableReads: map[string]int{},
	}
}

func (f *fakeVolcano) start() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	f.t.Cleanup(server.Close)
	return server
}

func (f *fakeVolcano) cloudDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{CommandPathPrefix: "volcano cloud", HTTPClient: server.Client(), APIBaseURL: server.URL}
}

func (f *fakeVolcano) localDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{
		CommandPathPrefix: "volcano",
		LocalMode:         true,
		HTTPClient:        server.Client(),
		ConfigLoader: func() (*cliconfig.Config, error) {
			return &cliconfig.Config{
				APIBaseURL:     server.URL,
				UserToken:      "local-token",
				CurrentProject: &cliconfig.ProjectConfig{ID: deployProjectID, Name: "local"},
			}, nil
		},
	}
}

func (f *fakeVolcano) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeVolcano) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	project := "/projects/" + deployProjectID
	path := r.URL.Path
	if f.failPath != "" && r.Method != http.MethodGet && strings.HasSuffix(path, f.failPath) {
		f.calls = append(f.calls, "failed "+strings.TrimPrefix(path, project))
		f.writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
		return
	}
	switch {
	case r.Method == http.MethodGet && path == "/functions/runtimes":
		f.writeJSON(w, http.StatusOK, runtimesPayload())
	case r.Method == http.MethodGet && path == project:
		f.writeJSON(w, http.StatusOK, map[string]any{"id": deployProjectID, "name": "Beta"})
	case r.Method == http.MethodGet && path == project+"/source-export":
		f.reads = append(f.reads, "source-export")
		f.writeJSON(w, http.StatusOK, map[string]any{"mode": f.sourceMode})
	case r.Method == http.MethodPost && path == project+"/variables":
		var body map[string]string
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.calls = append(f.calls, "variable "+body["name"])
		f.writeJSON(w, http.StatusCreated, map[string]any{
			"id": uuid.NewString(), "project_id": deployProjectID, "name": body["name"], "value": body["value"],
			"created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z",
		})
	case r.Method == http.MethodPut && path == project+"/config":
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.calls = append(f.calls, "config")
		f.configs = append(f.configs, body)
		f.writeJSON(w, http.StatusOK, map[string]any{
			"results": []any{}, "skipped": []any{}, "missing": []any{},
			"summary": map[string]any{"created": 0, "updated": 0, "deleted": 0, "unchanged": 0, "errors": 0, "skipped": 0, "missing": 0},
		})
	case r.Method == http.MethodGet && path == project+"/functions":
		f.writeJSON(w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false, "page": 1, "limit": 100, "total": 0})
	case r.Method == http.MethodPost && path == project+"/functions/batch":
		require.NoError(f.t, r.ParseMultipartForm(4<<20))
		var items []struct {
			Name string `json:"name"`
		}
		require.NoError(f.t, json.Unmarshal([]byte(r.FormValue("functions")), &items))
		data := make([]any, 0, len(items))
		for _, item := range items {
			f.calls = append(f.calls, "function "+item.Name)
			data = append(data, f.deployed(item.Name))
		}
		f.writeJSON(w, http.StatusAccepted, map[string]any{"batch_id": uuid.NewString(), "data": data})
	case r.Method == http.MethodPost && path == project+"/functions":
		require.NoError(f.t, r.ParseMultipartForm(4<<20))
		name := r.FormValue("name")
		f.calls = append(f.calls, "function "+name)
		f.writeJSON(w, http.StatusCreated, f.deployed(name))
	case r.Method == http.MethodPost && path == project+"/durable-functions":
		require.NoError(f.t, r.ParseMultipartForm(4<<20))
		name := r.FormValue("name")
		f.calls = append(f.calls, "durable "+name)
		payload := f.deployed(name)
		payload["visibility"] = "private"
		f.writeJSON(w, http.StatusCreated, payload)
	case r.Method == http.MethodPost && path == project+"/frontends":
		require.NoError(f.t, r.ParseMultipartForm(4<<20))
		name := r.FormValue("name")
		f.calls = append(f.calls, "frontend "+name)
		f.frontends[name] = r.MultipartForm.Value
		f.archives[name] = f.archiveNames(r.MultipartForm.File["archive"])
		f.writeJSON(w, http.StatusCreated, f.deployed(name))
	case r.Method == http.MethodGet && strings.HasPrefix(path, project+"/frontends/"):
		id := strings.TrimPrefix(path, project+"/frontends/")
		for name, resource := range f.resources {
			if resource.String() != id {
				continue
			}
			f.reads = append(f.reads, "frontend "+name)
			payload := f.deployed(name)
			payload["site_url"] = "https://" + name + ".frontends.example.test"
			f.writeJSON(w, http.StatusOK, payload)
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, project+"/durable-functions/"):
		id := strings.TrimPrefix(path, project+"/durable-functions/")
		for name, resource := range f.resources {
			if resource.String() != id {
				continue
			}
			read := f.durableReads[name]
			f.durableReads[name]++
			payload := f.deployed(name)
			payload["visibility"] = "private"
			payload["status"] = "active"
			if sequence := f.durableStatuses[name]; len(sequence) > 0 {
				payload["status"] = sequence[min(read, len(sequence)-1)]
			}
			f.writeJSON(w, http.StatusOK, payload)
			return
		}
		http.NotFound(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, project+"/databases/"):
		name := strings.TrimPrefix(path, project+"/databases/")
		f.reads = append(f.reads, "database "+name)
		f.writeJSON(w, http.StatusOK, map[string]any{
			"id": uuid.NewString(), "project_id": deployProjectID, "name": name, "status": "provisioning",
			"created_at": "2026-10-01T00:00:00Z", "updated_at": "2026-10-01T00:00:00Z",
		})
	case r.Method == http.MethodGet && path == project+"/deployments":
		f.reads = append(f.reads, "deployments")
		f.writeJSON(w, http.StatusOK, f.feed())
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, path)
		http.NotFound(w, r)
	}
}

// deployed returns the resource payload for a deploy of name, with a new
// current deployment.
func (f *fakeVolcano) deployed(name string) map[string]any {
	if _, ok := f.resources[name]; !ok {
		f.resources[name] = uuid.New()
	}
	if _, ok := f.deployments[name]; !ok {
		f.deployments[name] = uuid.New()
	}
	return map[string]any{
		"id":                    f.resources[name].String(),
		"project_id":            deployProjectID,
		"name":                  name,
		"runtime":               "nodejs24.x",
		"handler":               "handler",
		"framework":             "nextjs",
		"status":                "provisioning",
		"current_deployment_id": f.deployments[name].String(),
		"created_at":            "2026-10-01T00:00:00Z",
		"updated_at":            "2026-10-01T00:00:00Z",
	}
}

func (f *fakeVolcano) feed() map[string]any {
	poll := f.polls
	f.polls++
	created := time.Now().UTC()
	data := make([]any, 0, len(f.deployments))
	for name, id := range f.deployments {
		if f.unlisted[name] {
			continue
		}
		status := "active"
		if sequence := f.statuses[name]; len(sequence) > 0 {
			status = sequence[min(poll, len(sequence)-1)]
		}
		successor := f.queuedBehind[name]
		if sequence := f.superseding[name]; len(sequence) > 0 {
			status = "superseded"
			successor = sequence
		}
		if len(successor) > 0 {
			if _, ok := f.successors[name]; !ok {
				f.successors[name] = uuid.New()
			}
			data = append(data, f.feedEntry(name, f.successors[name], successor[min(poll, len(successor)-1)], created.Add(time.Second)))
		}
		data = append(data, f.feedEntry(name, id, status, created))
	}
	return map[string]any{"data": data, "has_more": false, "page": 1, "limit": 100, "total": len(data)}
}

func (f *fakeVolcano) archiveNames(files []*multipart.FileHeader) []string {
	require.Len(f.t, files, 1)
	file, err := files[0].Open()
	require.NoError(f.t, err)
	defer file.Close()
	gz, err := gzip.NewReader(file)
	require.NoError(f.t, err)
	defer gz.Close()
	var names []string
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		require.NoError(f.t, err)
		names = append(names, header.Name)
	}
}

func (f *fakeVolcano) feedEntry(name string, id uuid.UUID, status string, created time.Time) map[string]any {
	deployment := map[string]any{
		"id":            id.String(),
		"project_id":    deployProjectID,
		"status":        status,
		"operation":     "create",
		"deploy_source": "cli",
		"resource":      map[string]any{"id": f.resources[name].String(), "name": name, "type": "function"},
		"created_at":    created.Format(time.RFC3339),
		"updated_at":    created.Format(time.RFC3339),
	}
	if status == "failed" {
		deployment["error_message"] = "next build exited with code 1"
	}
	return deployment
}

func (f *fakeVolcano) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(f.t, json.NewEncoder(w).Encode(value))
}

func runtimesPayload() map[string]any {
	return map[string]any{
		"runtimes": []any{
			map[string]any{
				"name":            "nodejs24.x",
				"language":        "javascript",
				"default":         true,
				"durable_capable": true,
				"deployment": map[string]any{
					"file_extensions":      []string{".js", ".mjs"},
					"entrypoint":           "index.js",
					"handler":              "handler",
					"dependency_manifests": []string{"package.json"},
				},
			},
		},
	}
}

// tickNow is a ticker that is always ready, so a wait polls again at once.
type tickNow struct{}

func (tickNow) C() <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}

func (tickNow) Reset(time.Duration) {}

func (tickNow) Stop() {}
