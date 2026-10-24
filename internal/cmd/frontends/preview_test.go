package frontends

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	previewID       = "77777777-7777-4777-8777-777777777777"
	otherPreviewID  = "88888888-8888-4888-8888-888888888888"
	previewBasePath = "/projects/" + frontendProjectID + "/frontends/" + frontendID
)

// previewServer is a fake of the API's frontend list, previews and serving
// state, recording what the CLI sent.
type previewServer struct {
	t        *testing.T
	mu       sync.Mutex
	serving  map[string]any
	form     map[string]string
	archived int
	promoted map[string]any
	rolled   map[string]any
	conflict bool
	deleted  bool
}

func newPreviewServer(t *testing.T) (*previewServer, *httptest.Server) {
	t.Helper()
	p := &previewServer{t: t, serving: map[string]any{"revision": 4, "candidate_id": nil, "previous_candidate_id": nil}}
	server := httptest.NewServer(http.HandlerFunc(p.handle))
	t.Cleanup(server.Close)
	return p, server
}

func previewPayload(id, name, status string) map[string]any {
	return map[string]any{
		"id": id, "frontend_id": frontendID, "name": name, "status": status,
		"url":        "https://" + id + ".frontends.example.com",
		"created_at": "2026-10-23T10:00:00Z", "updated_at": "2026-10-23T10:00:00Z",
		"configuration_status": "current",
		"deployment": map[string]any{
			"id": frontendDeploymentID, "status": "active", "configuration_digest": "sha256:x",
			"created_at": "2026-10-23T10:00:00Z",
		},
	}
}

func (p *previewServer) handle(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
		writeFrontendCommandJSON(p.t, w, http.StatusOK, map[string]any{
			"data": []any{frontendCommandPayload(frontendID, "web")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
		})
	case r.Method == http.MethodGet && r.URL.Path == previewBasePath+"/candidates":
		writeFrontendCommandJSON(p.t, w, http.StatusOK, map[string]any{"data": []any{
			previewPayload(previewID, "redesign", "ready"), previewPayload(otherPreviewID, "beta", "creating"),
		}})
	case r.Method == http.MethodPost && r.URL.Path == previewBasePath+"/candidates":
		p.readForm(r)
		writeFrontendCommandJSON(p.t, w, http.StatusAccepted, previewPayload(previewID, p.form["name"], "creating"))
	case r.Method == http.MethodPost && r.URL.Path == previewBasePath+"/candidates/"+previewID+"/deployments":
		p.readForm(r)
		writeFrontendCommandJSON(p.t, w, http.StatusAccepted, map[string]any{
			"id": frontendDeploymentID, "status": "provisioning", "configuration_digest": "sha256:x",
			"created_at": "2026-10-23T10:00:00Z",
		})
	case r.Method == http.MethodDelete && r.URL.Path == previewBasePath+"/candidates/"+previewID:
		p.deleted = true
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && r.URL.Path == previewBasePath+"/serving":
		writeFrontendCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodPost && r.URL.Path == previewBasePath+"/candidates/"+previewID+"/promote":
		p.promoted = p.readJSON(r)
		if p.conflict {
			writeFrontendCommandJSON(p.t, w, http.StatusConflict, map[string]any{"error": "the candidate's configuration changed"})
			return
		}
		p.serving = map[string]any{"revision": 5, "candidate_id": previewID, "previous_candidate_id": nil}
		writeFrontendCommandJSON(p.t, w, http.StatusOK, p.serving)
	case r.Method == http.MethodPost && r.URL.Path == previewBasePath+"/serving/rollback":
		p.rolled = p.readJSON(r)
		p.serving = map[string]any{"revision": 6, "candidate_id": nil, "previous_candidate_id": previewID}
		writeFrontendCommandJSON(p.t, w, http.StatusOK, p.serving)
	default:
		http.NotFound(w, r)
	}
}

func (p *previewServer) readForm(r *http.Request) {
	require.NoError(p.t, r.ParseMultipartForm(32<<20))
	p.form = map[string]string{}
	for key, values := range r.MultipartForm.Value {
		p.form[key] = values[0]
	}
	if files := r.MultipartForm.File["archive"]; len(files) == 1 {
		file, err := files[0].Open()
		require.NoError(p.t, err)
		defer func() { _ = file.Close() }()
		data, err := io.ReadAll(file)
		require.NoError(p.t, err)
		p.archived = len(data)
	}
}

func (p *previewServer) readJSON(r *http.Request) map[string]any {
	var body map[string]any
	require.NoError(p.t, json.NewDecoder(r.Body).Decode(&body))
	return body
}

func previewProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"web","dependencies":{"next":"15.0.0"}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "next.config.js"), []byte("module.exports = {}\n"), 0o600))
	return dir
}

func runPreview(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	return executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
}

func TestPreviewCreateUploadsTheDirectoryWithItsNameAndExpiry(t *testing.T) {
	p, server := newPreviewServer(t)

	out, err := runPreview(t, server, "preview", "create", "web", "--name", "redesign", "--path", previewProject(t), "--ttl", "72h")

	require.NoError(t, err)
	assert.Equal(t, "redesign", p.form["name"])
	assert.Equal(t, "259200", p.form["ttl_seconds"], "a duration is sent as seconds")
	assert.Positive(t, p.archived, "the packaged directory is uploaded")
	assert.Contains(t, out, "Preview 'redesign' deployment started")
	assert.Contains(t, out, "frontends preview get web redesign")
}

func TestPreviewCreateWithoutAnExpiryKeepsThePreviewUntilDeleted(t *testing.T) {
	p, server := newPreviewServer(t)

	_, err := runPreview(t, server, "preview", "create", "web", "--name", "redesign", "--path", previewProject(t))

	require.NoError(t, err)
	assert.NotContains(t, p.form, "ttl_seconds")
}

func TestPreviewCreateRefusesAnExpiryTheAPIWouldRefuse(t *testing.T) {
	_, server := newPreviewServer(t)

	_, err := runPreview(t, server, "preview", "create", "web", "--name", "redesign", "--path", previewProject(t), "--ttl", (31 * 24 * time.Hour).String())

	require.ErrorContains(t, err, "--ttl must be between")
}

func TestPreviewCreateNeedsAName(t *testing.T) {
	_, server := newPreviewServer(t)

	_, err := runPreview(t, server, "preview", "create", "web", "--path", previewProject(t))

	require.ErrorContains(t, err, `required flag(s) "name" not set`)
}

func TestPreviewListShowsEachPreviewWithItsStatusAndURL(t *testing.T) {
	_, server := newPreviewServer(t)

	out, err := runPreview(t, server, "preview", "list", "web")

	require.NoError(t, err)
	assert.Contains(t, out, "redesign")
	assert.Contains(t, out, "ready")
	assert.Contains(t, out, "beta")
	assert.Contains(t, out, "creating")
	assert.Contains(t, out, "https://"+previewID+".frontends.example.com")
	assert.Contains(t, out, "never", "a preview with no expiry says so")
}

func TestPreviewGetFindsAPreviewByNameOrByID(t *testing.T) {
	_, server := newPreviewServer(t)

	for _, identifier := range []string{"redesign", previewID} {
		out, err := runPreview(t, server, "preview", "get", "web", identifier)

		require.NoError(t, err, identifier)
		assert.Contains(t, out, "Name:     redesign", identifier)
		assert.Contains(t, out, "Variables: current", identifier)
	}
	_, err := runPreview(t, server, "preview", "get", "web", "nonesuch")
	require.ErrorContains(t, err, `candidate "nonesuch" of frontend "web"`)
}

func TestPreviewDeploySendsOnlyTheArchive(t *testing.T) {
	p, server := newPreviewServer(t)

	out, err := runPreview(t, server, "preview", "deploy", "web", "redesign", "--path", previewProject(t))

	require.NoError(t, err)
	assert.Empty(t, p.form, "the API takes only the archive when redeploying")
	assert.Positive(t, p.archived)
	assert.Contains(t, out, "Preview 'redesign' deployment started")
	assert.Contains(t, out, "Deployment: "+frontendDeploymentID)
}

func TestPreviewDeleteDeletesTheNamedPreview(t *testing.T) {
	p, server := newPreviewServer(t)

	out, err := runPreview(t, server, "preview", "delete", "web", "redesign")

	require.NoError(t, err)
	assert.True(t, p.deleted)
	assert.Contains(t, out, "Preview 'redesign' is being deleted")
}

func TestPreviewPromoteNamesTheRevisionItJustRead(t *testing.T) {
	p, server := newPreviewServer(t)

	out, err := runPreview(t, server, "preview", "promote", "web", "redesign")

	require.NoError(t, err)
	assert.EqualValues(t, 4, p.promoted["expected_revision"], "a competing promotion would make the API refuse it")
	assert.Contains(t, out, "Frontend now serves preview 'redesign'")
	assert.Contains(t, out, "Revision: 5")
}

func TestPreviewPromoteReportsWhyTheAPIRefusedIt(t *testing.T) {
	p, server := newPreviewServer(t)
	p.conflict = true

	_, err := runPreview(t, server, "preview", "promote", "web", "redesign")

	require.ErrorContains(t, err, "failed to promote candidate")
	require.ErrorContains(t, err, "configuration changed")
}

func TestServingNamesThePreviewTheFrontendServes(t *testing.T) {
	p, server := newPreviewServer(t)
	p.serving = map[string]any{"revision": 5, "candidate_id": previewID, "previous_candidate_id": otherPreviewID}

	out, err := runPreview(t, server, "serving", "web")

	require.NoError(t, err)
	assert.Contains(t, out, "Frontend 'web' serves preview 'redesign' (revision 5)")
	assert.Contains(t, out, "A rollback returns to preview 'beta'")
}

func TestServingSaysWhenTheFrontendServesItsOwnBuild(t *testing.T) {
	_, server := newPreviewServer(t)

	out, err := runPreview(t, server, "serving", "web")

	require.NoError(t, err)
	assert.Contains(t, out, "Frontend 'web' serves its own build (revision 4)")
}

func TestRollbackNamesTheRevisionAndCanReturnToProduction(t *testing.T) {
	p, server := newPreviewServer(t)

	out, err := runPreview(t, server, "rollback", "web", "--to-production")

	require.NoError(t, err)
	assert.EqualValues(t, 4, p.rolled["expected_revision"])
	assert.Equal(t, true, p.rolled["to_production"])
	assert.Contains(t, out, "Frontend 'web' rolled back")
	assert.Contains(t, out, "serves its own build (revision 6)")

	_, err = runPreview(t, server, "rollback", "web")
	require.NoError(t, err)
	assert.NotContains(t, p.rolled, "to_production", "a plain rollback does not ask for production")
}
