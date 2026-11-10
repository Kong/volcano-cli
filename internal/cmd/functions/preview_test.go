package functions

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	deleted  bool
	// deployed counts redeploys of the listed preview.
	deployed int
	// routeMissing answers every preview route with a bare 404, as a server
	// older than function previews does; unavailable answers them with
	// feature_unavailable.
	routeMissing bool
	unavailable  bool
	// listed replaces the one preview the server lists, when set.
	listed []any
}

func newFunctionPreviewServer(t *testing.T) (*functionPreviewServer, *httptest.Server) {
	t.Helper()
	p := &functionPreviewServer{t: t, serving: map[string]any{
		"revision": 4, "candidate_id": nil, "previous_candidate_id": nil, "activation": nil,
		"production_environment": map[string]any{"status": "current"},
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
		"deployment": map[string]any{
			"id": "99999999-9999-4999-8999-999999999999", "status": "active", "operation": "deploy",
			"image_digests":        []any{map[string]any{"region": "us-east-1", "digest": "sha256:aa"}},
			"configuration_digest": "sha256:x",
			"regions":              []string{"us-east-1"}, "created_at": "2026-11-07T10:00:00Z",
		},
	}
}

func (p *functionPreviewServer) handle(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if writeFunctionRuntimesCommandResponse(w, r) {
		return
	}
	previewRoute := strings.HasPrefix(r.URL.Path, functionPreviewBasePath+"/candidates") ||
		strings.HasPrefix(r.URL.Path, functionPreviewBasePath+"/serving")
	switch {
	case previewRoute && p.routeMissing:
		http.NotFound(w, r)
	case previewRoute && p.unavailable:
		writeFunctionCommandJSON(p.t, w, http.StatusNotFound, map[string]any{
			"error": "function previews are not available", "code": "feature_unavailable",
		})
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
		writeFunctionCommandJSON(p.t, w, http.StatusOK, map[string]any{
			"data": []any{functionCommandPayload(functionID, "api")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath:
		writeFunctionCommandJSON(p.t, w, http.StatusOK, functionCommandPayload(functionID, "api"))
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/candidates":
		listed := []any{functionPreviewPayload("ready")}
		if p.listed != nil {
			listed = p.listed
		}
		writeFunctionCommandJSON(p.t, w, http.StatusOK, map[string]any{"data": listed})
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/candidates":
		p.readForm(r)
		created := functionPreviewPayload("creating")
		created["name"] = p.form["name"]
		writeFunctionCommandJSON(p.t, w, http.StatusAccepted, created)
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
		p.serving = map[string]any{"revision": 5, "candidate_id": functionPreviewID, "previous_candidate_id": nil, "activation": nil, "production_environment": map[string]any{"status": "current"}}
		writeFunctionCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/serving/rollback":
		p.rolled = p.readJSON(r)
		p.serving = map[string]any{"revision": 6, "candidate_id": nil, "previous_candidate_id": functionPreviewID, "activation": nil, "production_environment": map[string]any{"status": "current"}}
		writeFunctionCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID:
		writeFunctionCommandJSON(p.t, w, http.StatusOK, functionPreviewPayload("ready"))
	case r.Method == http.MethodGet && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID+"/deployments":
		older := functionPreviewPayload("ready")["deployment"].(map[string]any)
		writeFunctionCommandJSON(p.t, w, http.StatusOK, map[string]any{"data": []any{older}})
	case r.Method == http.MethodDelete && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID:
		p.deleted = true
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodPost && r.URL.Path == functionPreviewBasePath+"/candidates/"+functionPreviewID+"/deployments":
		p.readForm(r)
		p.deployed++
		writeFunctionCommandJSON(p.t, w, http.StatusAccepted, functionPreviewPayload("deploying")["deployment"])
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
	return runFunctionPreviewWithInput(t, server, "", args...)
}

func runFunctionPreviewWithInput(t *testing.T, server *httptest.Server, input string, args ...string) (string, error) {
	t.Helper()
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader(input))
	return executeFunctionsCommand(t, cmd, args...)
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

func TestFunctionPreviewCreatePrintsAGetCommandThatCanBePasted(t *testing.T) {
	_, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "create", "api", "--name", "checkout v2",
		"--path", functionPreviewProject(t))

	require.NoError(t, err)
	assert.Contains(t, out, "functions preview get api 'checkout v2'")
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

func TestFunctionPreviewPromotePrefersAnExactNameOverAnotherPreviewsSlug(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	older := functionPreviewPayload("ready")
	older["id"] = "66666666-6666-4666-8666-666666666666"
	older["name"] = "checkout v2"
	older["slug"] = "checkout-v2"
	named := functionPreviewPayload("ready")
	named["slug"] = "checkout-v2-2"
	p.listed = []any{older, named}

	out, err := runFunctionPreview(t, server, "preview", "promote", "api", "checkout-v2")

	require.NoError(t, err)
	assert.NotNil(t, p.promoted, "the preview named checkout-v2 is promoted, not the one whose slug matches")
	assert.Contains(t, out, "Function now runs preview 'checkout-v2'")
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
	assert.Contains(t, out, "Image:     us-east-1 sha256:aa")
	assert.Contains(t, out, "https://"+functionPreviewID+".functions.example.com")
}

func TestFunctionPreviewTTLSecondsBounds(t *testing.T) {
	for _, tc := range []struct {
		ttl     time.Duration
		want    int
		wantErr string
	}{
		{ttl: 0, want: 0},
		{ttl: time.Second, want: 1},
		{ttl: 72 * time.Hour, want: 259200},
		{ttl: maxPreviewTTL, want: 2592000},
		{ttl: 1500 * time.Millisecond, want: 1},
		{ttl: maxPreviewTTL + time.Second, wantErr: "--ttl must be between 0 and"},
		{ttl: -time.Second, wantErr: "--ttl must be between 0 and"},
		{ttl: 500 * time.Millisecond, wantErr: "--ttl must be at least 1s"},
	} {
		got, err := previewTTLSeconds(tc.ttl)
		if tc.wantErr != "" {
			require.ErrorContains(t, err, tc.wantErr, tc.ttl.String())
			continue
		}
		require.NoError(t, err, tc.ttl.String())
		assert.Equal(t, tc.want, got, tc.ttl.String())
	}
}

func TestFunctionPreviewCreateRefusesATTLOverTheLimitBeforeUploading(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	_, err := runFunctionPreview(t, server, "preview", "create", "api", "--name", "x",
		"--path", functionPreviewProject(t), "--ttl", "721h")

	require.ErrorContains(t, err, "--ttl must be between 0 and 720h")
	assert.Empty(t, p.form, "nothing was created")
}

func TestFunctionPreviewDeleteAsksFirstAndCancelsOnNo(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreviewWithInput(t, server, "no\n", "preview", "delete", "api", "checkout-v2")

	require.NoError(t, err)
	assert.False(t, p.deleted)
	assert.Contains(t, out, "Delete preview 'checkout-v2'?")
	assert.Contains(t, out, "Delete cancelled.")
	assert.NotContains(t, out, "is being deleted")
}

func TestFunctionPreviewDeleteDeletesOnceConfirmed(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreviewWithInput(t, server, "y\n", "preview", "delete", "api", "checkout-v2")

	require.NoError(t, err)
	assert.True(t, p.deleted)
	assert.Contains(t, out, "Preview 'checkout-v2' is being deleted")
}

func TestFunctionPreviewDeleteWithYesSkipsThePrompt(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "delete", "api", "checkout-v2", "--yes")

	require.NoError(t, err)
	assert.True(t, p.deleted)
	assert.NotContains(t, out, "Delete preview")
}

func TestFunctionPreviewDeleteOfAMissingPreviewAsksNothing(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreviewWithInput(t, server, "y\n", "preview", "delete", "api", "nope")

	require.ErrorContains(t, err, `preview "nope" of function "api"`)
	assert.False(t, p.deleted)
	assert.NotContains(t, out, "Delete preview")
}

func TestFunctionPreviewDeployUploadsTheSourceToTheNamedPreview(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "deploy", "api", "checkout-v2", "--path", functionPreviewProject(t))

	require.NoError(t, err)
	assert.Equal(t, 1, p.deployed)
	assert.Positive(t, p.uploaded, "the function's source is uploaded as code")
	assert.NotContains(t, p.form, "name", "a redeploy sends only the source")
	assert.Contains(t, out, "Preview 'checkout-v2' deployment started")
	assert.Contains(t, out, "Deployment: 99999999-9999-4999-8999-999999999999")
}

func TestFunctionPreviewDeployFindsThePreviewBeforePackaging(t *testing.T) {
	p, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "deploy", "api", "nope", "--path", functionPreviewProject(t))

	require.ErrorContains(t, err, `preview "nope" of function "api"`)
	assert.Zero(t, p.deployed)
	assert.NotContains(t, out, "Packaged", "nothing is packaged for a preview that does not exist")
}

func TestFunctionPreviewListShowsEachPreview(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	kept := functionPreviewPayload("ready")
	kept["id"] = "66666666-6666-4666-8666-666666666666"
	kept["name"] = "older"
	kept["kept_for_rollback"] = true
	kept["expires_at"] = "2026-11-10T10:00:00Z"
	delete(kept, "url")
	p.listed = []any{functionPreviewPayload("ready"), kept}

	out, err := runFunctionPreview(t, server, "preview", "list", "api")

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "NAME")
	assert.Contains(t, lines[1], "checkout-v2")
	assert.Contains(t, lines[1], "never")
	assert.Contains(t, lines[2], "older")
	assert.Contains(t, lines[2], "rollback")
	assert.NotContains(t, lines[2], "never")
}

func TestFunctionPreviewListSaysSoWhenThereAreNone(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	p.listed = []any{}

	out, err := runFunctionPreview(t, server, "preview", "list", "api")

	require.NoError(t, err)
	assert.Equal(t, "No previews.\n", out)
}

func TestFunctionPreviewListGetAndServingEmitJSON(t *testing.T) {
	_, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "list", "api", "--json")
	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "checkout-v2", listed[0]["name"])

	out, err = runFunctionPreview(t, server, "preview", "get", "api", "checkout-v2", "--json")
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, functionPreviewID, got["id"])

	out, err = runFunctionPreview(t, server, "serving", "api", "--json")
	require.NoError(t, err)
	var serving map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &serving))
	assert.InDelta(t, 4, serving["revision"], 0)
}

func TestFunctionPreviewOnAServerWithoutPreviewsSaysSoBeforePackaging(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	p.routeMissing = true

	for _, args := range [][]string{
		{"preview", "create", "api", "--name", "x", "--path", functionPreviewProject(t)},
		{"preview", "deploy", "api", "checkout-v2", "--path", functionPreviewProject(t)},
		{"preview", "list", "api"},
		{"serving", "api"},
		{"rollback", "api"},
	} {
		out, err := runFunctionPreview(t, server, args...)

		require.ErrorContains(t, err, "this Volcano server doesn't support function previews yet", args)
		assert.NotContains(t, out, "Packaged", args)
	}
	assert.Empty(t, p.form, "nothing was uploaded")
}

func TestFunctionPreviewFeatureUnavailableSaysPreviews(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	p.unavailable = true

	_, err := runFunctionPreview(t, server, "preview", "list", "api")

	require.ErrorContains(t, err, "function previews aren't available for this project yet")
}

func TestFunctionPreviewDeploymentsListsAPreviewsDeployments(t *testing.T) {
	_, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "preview", "deployments", "api", "checkout-v2")

	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "OPERATION")
	assert.Contains(t, lines[1], "99999999-9999-4999-8999-999999999999")
	assert.Contains(t, lines[1], "active")
	assert.Contains(t, lines[1], "us-east-1")

	out, err = runFunctionPreview(t, server, "preview", "deployments", "api", "checkout-v2", "--json")
	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "deploy", listed[0]["operation"])
}

func TestFunctionServingSaysWhenTheOwnBuildsVariablesAreOutOfDate(t *testing.T) {
	p, server := newFunctionPreviewServer(t)
	p.serving = map[string]any{
		"revision": 5, "candidate_id": functionPreviewID, "previous_candidate_id": nil,
		"production_environment": map[string]any{"status": "out_of_date", "out_of_date_since": "2026-11-10T10:00:00Z"},
		"activation": map[string]any{
			"id": "88888888-8888-4888-8888-888888888888", "kind": "promotion", "status": "active", "revision": 5,
			"candidate_id": functionPreviewID, "image_digests": []any{map[string]any{"digest": "sha256:bb"}},
		},
	}

	out, err := runFunctionPreview(t, server, "serving", "api")

	require.NoError(t, err)
	assert.Contains(t, out, "Function 'api' runs preview 'checkout-v2' (revision 5)")
	assert.Contains(t, out, "Latest promotion: active (revision 5)")
	assert.Contains(t, out, "Image: sha256:bb")
	assert.Contains(t, out, "Own build's variables: out of date since ")
	assert.Contains(t, out, "a rollback to production synchronizes them")

	out, err = runFunctionPreview(t, server, "serving", "api", "--json")
	require.NoError(t, err)
	var serving map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &serving))
	assert.Equal(t, map[string]any{"status": "out_of_date", "out_of_date_since": "2026-11-10T10:00:00Z"}, serving["production_environment"])
}

func TestFunctionServingSaysNothingOfCurrentVariables(t *testing.T) {
	_, server := newFunctionPreviewServer(t)

	out, err := runFunctionPreview(t, server, "serving", "api")

	require.NoError(t, err)
	assert.NotContains(t, out, "variables")
}
