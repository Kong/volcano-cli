package functions

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	functionPreviewID       = "77777777-7777-4777-8777-777777777777"
	functionPreviewBasePath = "/projects/" + functionProjectID + "/functions/" + functionID
)

// functionPreviewServer fakes the function list, previews and serving state,
// recording what the CLI sent.
type functionPreviewServer struct {
	t        *testing.T
	mu       sync.Mutex
	serving  map[string]any
	form     map[string]string
	uploaded int
	promoted map[string]any
	rolled   map[string]any
	conflict bool
}

func newFunctionPreviewServer(t *testing.T) (*functionPreviewServer, *httptest.Server) {
	t.Helper()
	p := &functionPreviewServer{t: t, serving: map[string]any{
		"revision": 4, "candidate_id": nil, "previous_candidate_id": nil, "activation": nil,
	}}
	server := httptest.NewServer(http.HandlerFunc(p.handle))
	t.Cleanup(server.Close)
	return p, server
}

func functionPreviewPayload(status string) map[string]any {
	return map[string]any{
		"id": functionPreviewID, "function_id": functionID, "name": "checkout-v2", "slug": "checkout-v2",
		"status": status, "exposure": "private", "url": "https://" + functionPreviewID + ".functions.example.com",
		"created_at": "2026-11-07T10:00:00Z", "updated_at": "2026-11-07T10:00:00Z",
		"configuration_status": "current", "serving": false, "kept_for_rollback": false,
		"latest_deployment": map[string]any{
			"id": "99999999-9999-4999-8999-999999999999", "status": "active", "operation": "deploy",
			"image_uri": "us-east-1=repo@sha256:aa", "configuration_digest": "sha256:x",
			"regions": []string{"us-east-1"}, "created_at": "2026-11-07T10:00:00Z",
		},
	}
}

func (p *functionPreviewServer) handle(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if writeFunctionRuntimesCommandResponse(w, r) {
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
		writeFunctionCommandJSON(p.t, w, http.StatusOK, map[string]any{
			"data": []any{functionCommandPayload(functionID, "api")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath:
		writeFunctionCommandJSON(p.t, w, http.StatusOK, functionCommandPayload(functionID, "api"))
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/candidates":
		writeFunctionCommandJSON(p.t, w, http.StatusOK, map[string]any{"data": []any{functionPreviewPayload("ready")}})
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/candidates":
		p.readForm(r)
		writeFunctionCommandJSON(p.t, w, http.StatusAccepted, functionPreviewPayload("creating"))
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/serving":
		writeFunctionCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID+"/promote":
		p.promoted = p.readJSON(r)
		if p.conflict {
			writeFunctionCommandJSON(p.t, w, http.StatusConflict, map[string]any{
				"error": "the frontend's configuration changed since the candidate was built", "code": "configuration_changed",
			})
			return
		}
		p.serving = map[string]any{"revision": 5, "candidate_id": functionPreviewID, "previous_candidate_id": nil, "activation": nil}
		writeFunctionCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/serving/rollback":
		p.rolled = p.readJSON(r)
		p.serving = map[string]any{"revision": 6, "candidate_id": nil, "previous_candidate_id": functionPreviewID, "activation": nil}
		writeFunctionCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID:
		writeFunctionCommandJSON(p.t, w, http.StatusOK, functionPreviewPayload("ready"))
	default:
		http.NotFound(w, r)
	}
}

func (p *functionPreviewServer) readForm(r *http.Request) {
	require.NoError(p.t, r.ParseMultipartForm(32<<20))
	p.form = map[string]string{}
	for key, values := range r.MultipartForm.Value {
		p.form[key] = values[0]
	}
	if files := r.MultipartForm.File["code"]; len(files) == 1 {
		file, err := files[0].Open()
		require.NoError(p.t, err)
		defer func() { _ = file.Close() }()
		data, err := io.ReadAll(file)
		require.NoError(p.t, err)
		p.uploaded = len(data)
	}
}

func (p *functionPreviewServer) readJSON(r *http.Request) map[string]any {
	var body map[string]any
	require.NoError(p.t, json.NewDecoder(r.Body).Decode(&body))
	return body
}

func functionPreviewProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "api.js"), []byte("exports.handler = async () => ({ ok: true });\n"), 0o600))
	return dir
}

func runFunctionPreview(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	return executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
}

func TestFunctionPreviewCreateUploadsTheFunctionsSourceWithItsNameAndExpiry(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "create", "api", "--name", "checkout-v2",
		"--path", functionPreviewProject(t), "--ttl", "72h")

	require.NoError(t, err)
	assert.Equal(t, "checkout-v2", p.form["name"])
	assert.Equal(t, "259200", p.form["ttl_seconds"])
	assert.Positive(t, p.uploaded, "the function's source is uploaded as code")
	assert.Contains(t, out, "Preview 'checkout-v2' deployment started")
}

func TestFunctionPreviewCreateRefusesAFunctionWithNoSource(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	_, err := runFunctionPreview(t, server, "preview", "create", "api", "--name", "x", "--path", t.TempDir())

	require.ErrorContains(t, err, `no source for function "api"`)
	assert.Empty(t, p.form, "nothing was created")
}

func TestFunctionPreviewPromoteNamesTheRevisionItRead(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "promote", "api", "checkout-v2")

	require.NoError(t, err)
	assert.InDelta(t, 4, p.promoted["expected_revision"], 0)
	assert.Contains(t, out, "Function now runs preview 'checkout-v2'")
	assert.Contains(t, out, "Revision: 5")
}

func TestFunctionPreviewPromoteReportsTheRefusal(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	p.conflict = true

	_, err := runFunctionPreview(t, server, "preview", "promote", "api", "checkout-v2")

	require.ErrorContains(t, err, "configuration changed")
}

func TestFunctionRollbackToProductionSendsTheFlagAndShowsTheHeldPreview(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "rollback", "api", "--to-production")

	require.NoError(t, err)
	assert.Equal(t, true, p.rolled["to_production"])
	assert.InDelta(t, 4, p.rolled["expected_revision"], 0)
	assert.Contains(t, out, "Function 'api' runs its own build (revision 6)")
	assert.Contains(t, out, "A rollback returns to preview 'checkout-v2'")
}

func TestFunctionPreviewGetShowsTheImageAndVariables(t *testing.T) {
	_, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "get", "api", "checkout-v2")

	require.NoError(t, err)
	assert.Contains(t, out, "Variables: current")
	assert.Contains(t, out, "Image:     us-east-1=repo@sha256:aa")
	assert.Contains(t, out, "https://"+functionPreviewID+".functions.example.com")
}
