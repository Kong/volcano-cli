package project

import (
	"bytes"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	fromLinkID = "k3Vn8TqPz0aX"
	fromLink   = "https://volcano.dev/deploy/" + fromLinkID
	commitOne  = "0123456789abcdef0123456789abcdef01234567"
	commitTwo  = "fedcba9876543210fedcba9876543210fedcba98"
)

// fromServer is a fake API for `projects create --from`. Its handlers can be
// replaced per test; the counters say what reached it.
type fromServer struct {
	t       *testing.T
	mu      sync.Mutex
	preview func(call int) (int, any)
	create  func(call int, body map[string]any) (int, any)
	get     func(call int) (int, any)

	previews, creates, gets int
	previewAuth             []string
	createAuth              []string
	createBodies            []map[string]any
}

func newFromServer(t *testing.T) (*fromServer, *httptest.Server) {
	t.Helper()
	fake := &fromServer{t: t}
	fake.preview = func(int) (int, any) { return http.StatusOK, previewPayload(commitOne) }
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusCreated, installingProject("pending", "database", nil)
	}
	fake.get = func(int) (int, any) {
		return http.StatusOK, installingProject("ready", "ready", sourcePayload(commitOne))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		var status int
		var body any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/deploy-links/"+fromLinkID:
			fake.previews++
			fake.previewAuth = append(fake.previewAuth, r.Header.Get("Authorization"))
			status, body = fake.preview(fake.previews)
		case r.Method == http.MethodPost && r.URL.Path == "/projects":
			fake.creates++
			fake.createAuth = append(fake.createAuth, r.Header.Get("Authorization"))
			var decoded map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&decoded))
			fake.createBodies = append(fake.createBodies, decoded)
			status, body = fake.create(fake.creates, decoded)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectBetaID:
			fake.gets++
			status, body = fake.get(fake.gets)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			status, body = http.StatusNotFound, map[string]any{"error": "unexpected"}
		}
		writeProjectCommandJSON(t, w, status, body)
	}))
	t.Cleanup(server.Close)
	return fake, server
}

func previewPayload(commit string) map[string]any {
	return previewPayloadAt(commit, "apps/web")
}

func previewPayloadAt(commit, path string) map[string]any {
	return map[string]any{
		"id": fromLinkID,
		"repository": map[string]any{
			"full_name": "acme/notes-app", "owner": "acme", "name": "notes-app", "url": "https://github.com/acme/notes-app",
		},
		"branch":         "main",
		"commit_sha":     commit,
		"path":           path,
		"suggested_name": "notes-app",
		"frontends":      []string{"web"},
		"functions":      []string{"api", "worker"},
	}
}

func sourcePayload(commit string) map[string]any {
	return map[string]any{"kind": "github", "repository": "acme/notes-app", "commit_sha": commit}
}

func installingProject(status, phase string, source map[string]any) map[string]any {
	payload := projectCommandPayload(projectBetaID, "my-app", "active", nil)
	installation := map[string]any{"status": status, "phase": phase}
	if source != nil {
		installation["source"] = source
	}
	payload["template_installation"] = installation
	return payload
}

func fromDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		NewTicker:  func(time.Duration) cliruntime.Ticker { return fromImmediateTicker{} },
	}
}

type fromImmediateTicker struct{}

func (fromImmediateTicker) C() <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Now()
	return ch
}
func (fromImmediateTicker) Reset(time.Duration) {}
func (fromImmediateTicker) Stop()               {}

func loggedIn(t *testing.T) {
	t.Helper()
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
}

// runFrom runs `projects create my-app <args>` with stdin as given. Each line of
// stdin is read separately, as a terminal would deliver it.
func runFrom(t *testing.T, server *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := NewProjects(fromDeps(server))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(iotest.OneByteReader(strings.NewReader(stdin)))
	cmd.SetArgs(append([]string{"create", "my-app"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestCreateFromShowsThePreviewAndDeploysTheConfirmedCommit(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "y\n", "--from", fromLink)
	require.NoError(t, err)

	for _, want := range []string{
		"Repository: acme/notes-app (https://github.com/acme/notes-app)", "Branch: main", "Commit: " + commitOne,
		"Path: apps/web", "Frontends: web", "Functions: api, worker", "Volcano has not reviewed this code",
		"Deploy this commit?", "Project created: my-app (" + projectBetaID + ")",
		"Project ready: my-app", "Source: acme/notes-app@0123456",
		"volcano projects get " + projectBetaID, "volcano use " + projectBetaID,
	} {
		assert.Contains(t, out, want)
	}
	assert.Equal(t, []map[string]any{{
		"name": "my-app",
		"template_source": map[string]any{
			"deploy_link": map[string]any{"id": fromLinkID, "commit_sha": commitOne, "path": "apps/web"},
		},
	}}, fake.createBodies, "the project is pinned to the previewed commit")
}

// The preview route is public, so it carries no credential; the create does.
func TestCreateFromSendsNoCredentialForThePreview(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.NoError(t, err)

	assert.Equal(t, []string{""}, fake.previewAuth)
	assert.Equal(t, []string{"Bearer token"}, fake.createAuth)
}

func TestCreateFromAcceptsABareID(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLinkID, "--yes")
	require.NoError(t, err)
	assert.Equal(t, 1, fake.creates)
}

func TestCreateFromDeclinedCreatesNothing(t *testing.T) {
	for _, stdin := range []string{"n\n", "\n", ""} {
		t.Run(strings.TrimSpace(stdin)+"-", func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)

			out, err := runFrom(t, server, stdin, "--from", fromLink)
			require.NoError(t, err)
			assert.Zero(t, fake.creates)
			assert.Contains(t, out, "Cancelled.")
		})
	}
}

func TestCreateFromYesSkipsThePrompt(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.NoError(t, err)
	assert.Equal(t, 1, fake.creates)
	assert.NotContains(t, out, "Deploy this commit?")
}

func TestCreateFromCommitMustMatchThePreview(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes", "--commit", "fedcba9")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the deploy link is at commit "+commitOne+", not the fedcba9 that --commit expects")
	assert.Contains(t, err.Error(), "nothing was created")
	assert.Contains(t, out, "Commit: "+commitOne, "the commit it found is shown")
	assert.Zero(t, fake.creates)

	for _, commit := range []string{commitOne, "0123456", strings.ToUpper(commitOne[:10])} {
		_, err = runFrom(t, server, "", "--from", fromLink, "--yes", "--commit", commit)
		require.NoError(t, err, commit)
	}
	assert.Equal(t, 3, fake.creates)

	_, err = runFrom(t, server, "", "--from", fromLink, "--yes", "--commit", "xyz")
	require.ErrorContains(t, err, "invalid --commit")
}

// Interactively, a branch that moved shows the new commit and asks again.
func TestCreateFromChangedCommitAsksAgainWhenInteractive(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.create = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{"error": "the branch moved", "code": "commit_changed"}
		}
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(commitTwo))
	}
	fake.preview = func(call int) (int, any) {
		if call == 1 {
			return http.StatusOK, previewPayload(commitOne)
		}
		return http.StatusOK, previewPayload(commitTwo)
	}

	out, err := runFrom(t, server, "y\ny\n", "--from", fromLink)
	require.NoError(t, err)

	assert.Equal(t, 2, fake.creates)
	assert.Equal(t, 2, fake.previews)
	assert.Contains(t, out, "The repository changed since the preview")
	assert.Equal(t, 2, strings.Count(out, "Deploy this commit?"), "the new commit is confirmed on its own")
	assert.Contains(t, out, "Commit: "+commitTwo)
	second := fake.createBodies[1]["template_source"].(map[string]any)["deploy_link"].(map[string]any)
	assert.Equal(t, commitTwo, second["commit_sha"])
}

// The build root is confirmed with the commit, so it is sent exactly as the
// preview showed it: a path as is, and the repository root as an empty string.
func TestCreateFromSendsThePreviewsPath(t *testing.T) {
	for _, path := range []string{"apps/web", ""} {
		t.Run("path="+path, func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)
			fake.preview = func(int) (int, any) { return http.StatusOK, previewPayloadAt(commitOne, path) }

			out, err := runFrom(t, server, "", "--from", fromLink, "--yes")
			require.NoError(t, err)

			source := fake.createBodies[0]["template_source"].(map[string]any)["deploy_link"].(map[string]any)
			assert.Equal(t, map[string]any{"id": fromLinkID, "commit_sha": commitOne, "path": path}, source,
				"nothing else is sent")
			if path == "" {
				assert.NotContains(t, out, "Path:")
			} else {
				assert.Contains(t, out, "Path: "+path)
			}
		})
	}
}

// The owner pointing the link at another path is the same refusal as the
// branch moving. The new preview shows the new path, and the second attempt
// sends it.
func TestCreateFromChangedPathAsksAgainWithTheNewPath(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.create = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{"error": "the link changed", "code": "commit_changed"}
		}
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(commitOne))
	}
	fake.preview = func(call int) (int, any) {
		if call == 1 {
			return http.StatusOK, previewPayloadAt(commitOne, "apps/web")
		}
		return http.StatusOK, previewPayloadAt(commitOne, "apps/admin")
	}

	out, err := runFrom(t, server, "y\ny\n", "--from", fromLink)
	require.NoError(t, err)

	assert.Contains(t, out, "Path: apps/admin", "the new preview shows the new path")
	second := fake.createBodies[1]["template_source"].(map[string]any)["deploy_link"].(map[string]any)
	assert.Equal(t, "apps/admin", second["path"])
	assert.Equal(t, 2, strings.Count(out, "Deploy this commit?"))
}

func TestCreateFromChangedCommitStopsWhenTheRunAlreadyDecided(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"--yes", "--commit", commitOne}, {"--commit", commitOne}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)
			fake.create = func(int, map[string]any) (int, any) {
				return http.StatusConflict, map[string]any{"error": "the branch moved", "code": "commit_changed"}
			}
			fake.preview = func(call int) (int, any) {
				if call == 1 {
					return http.StatusOK, previewPayload(commitOne)
				}
				return http.StatusOK, previewPayload(commitTwo)
			}

			out, err := runFrom(t, server, "y\n", append([]string{"--from", fromLink}, args...)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "nothing was created and the new commit was not deployed")
			assert.Contains(t, err.Error(), "run the command again")
			assert.Contains(t, out, "Commit: "+commitTwo, "the new preview is printed")
			assert.Equal(t, 1, fake.creates, "the new commit is never sent")
		})
	}
}

func TestCreateFromWaitsThroughThePhasesToReady(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	phases := []string{"restore", "configure", "deploy"}
	fake.get = func(call int) (int, any) {
		if call <= len(phases) {
			return http.StatusOK, installingProject("running", phases[call-1], nil)
		}
		return http.StatusOK, installingProject("ready", "ready", sourcePayload(commitOne))
	}

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.NoError(t, err)

	assert.Equal(t, 4, fake.gets)
	last := -1
	for _, phase := range []string{"database", "restore", "configure", "deploy", "ready"} {
		at := strings.Index(out, "  - "+phase+"\n")
		if phase == "ready" {
			at = strings.Index(out, "ready\n")
		}
		require.Greater(t, at, last, "phase %s is printed in order:\n%s", phase, out)
		last = at
	}
	assert.Contains(t, out, "Project ready: my-app")
}

func TestCreateFromFailedInstallationExitsNonZeroWithAHint(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.get = func(int) (int, any) { return http.StatusOK, installingProject("failed", "deploy", nil) }

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.Error(t, err)
	assert.Contains(t, out, "installation failed during deploy")
	assert.Contains(t, err.Error(), "volcano projects get "+projectBetaID)
	assert.Contains(t, err.Error(), "volcano projects delete "+projectBetaID)
	assert.NotContains(t, out, "Project ready")
}

func TestCreateFromReadyInTheCreateResponseNeedsNoPolling(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(commitOne))
	}

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.NoError(t, err)
	assert.Zero(t, fake.gets)
	assert.Contains(t, out, "Project ready")
}

func TestCreateFromNoWaitReturnsRightAfterCreation(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "", "--from", fromLink, "--yes", "--no-wait")
	require.NoError(t, err)
	assert.Zero(t, fake.gets)
	assert.Contains(t, out, "Project created: my-app ("+projectBetaID+")")
	assert.Contains(t, out, "volcano projects get "+projectBetaID)
	assert.NotContains(t, out, "Project ready")
}

func TestCreateFromStopsWaitingAtTheTimeout(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.get = func(int) (int, any) { return http.StatusOK, installingProject("running", "deploy", nil) }
	deps := fromDeps(server)
	deps.NewTicker = func(time.Duration) cliruntime.Ticker { return neverTicker{} }
	fired := make(chan time.Time, 1)
	fired <- time.Now()
	deps.NewTimer = func(time.Duration) cliruntime.Timer { return fakeTimer{c: fired} }

	cmd := NewProjects(deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"create", "my-app", "--from", fromLink, "--yes"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped waiting after 30m0s")
	assert.Contains(t, err.Error(), "volcano projects get "+projectBetaID)
}

type neverTicker struct{}

func (neverTicker) C() <-chan time.Time { return nil }
func (neverTicker) Reset(time.Duration) {}
func (neverTicker) Stop()               {}

type fakeTimer struct{ c chan time.Time }

func (t fakeTimer) C() <-chan time.Time { return t.c }
func (fakeTimer) Stop()                 {}

func TestCreateFromRefusesBadFlagCombinations(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--from", fromLink, "--template", "trellini"}, "none of the others can be"},
		{[]string{"--commit", commitOne}, "--commit only applies with --from"},
		{[]string{"--yes"}, "--yes only applies with --from"},
		{[]string{"--no-wait"}, "--no-wait only applies with --from"},
		{[]string{"--from", "https://volcano.dev/projects/" + fromLinkID}, "invalid deploy link"},
		{[]string{"--from", "short"}, "invalid deploy link"},
	} {
		_, err := runFrom(t, server, "", tc.args...)
		require.Error(t, err, tc.args)
		assert.Contains(t, err.Error(), tc.want, tc.args)
	}
	assert.Zero(t, fake.previews+fake.creates, "nothing is sent for a refused command line")
}

// Signed out, the command stops before it asks the network anything.
func TestCreateFromNeedsASignedInAccount(t *testing.T) {
	setProjectCommandTestHome(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.Error(t, err)
	assert.Zero(t, fake.previews+fake.creates)
}

func TestCreateFromExplainsEachRefusal(t *testing.T) {
	for _, tc := range []struct {
		name    string
		preview bool
		status  int
		body    map[string]any
		want    []string
		absent  []string
	}{
		{
			"preview 404", true, http.StatusNotFound,
			map[string]any{"error": "SECRET reason: switched off"},
			[]string{"This deploy link is not available."},
			[]string{"SECRET", "switched off"},
		},
		{
			"preview gate", true, http.StatusNotFound,
			map[string]any{"error": "SECRET gate", "code": "feature_unavailable"},
			[]string{"This deploy link is not available."},
			[]string{"SECRET"},
		},
		{
			"preview 429", true, http.StatusTooManyRequests,
			map[string]any{"error": "slow down"},
			[]string{"HTTP 429: slow down", "Try again later"},
			nil,
		},
		{
			"preview 503", true, http.StatusServiceUnavailable,
			map[string]any{"error": "GitHub is rate limiting"},
			[]string{"HTTP 503: GitHub is rate limiting", "Try again shortly"},
			nil,
		},
		{
			"create 404", false, http.StatusNotFound,
			map[string]any{"error": "SECRET gone"},
			[]string{"This deploy link is not available."},
			[]string{"SECRET"},
		},
		{
			"create gate", false, http.StatusBadRequest,
			map[string]any{"error": "SECRET gate", "code": "feature_unavailable"},
			[]string{"Deploy links are not available in this environment."},
			[]string{"SECRET"},
		},
		{
			"create local mode", false, http.StatusBadRequest,
			map[string]any{"error": "project templates require cloud mode"},
			[]string{"HTTP 400: project templates require cloud mode"},
			nil,
		},
		{
			"create 403", false, http.StatusForbidden,
			map[string]any{"error": "project limit reached"},
			[]string{"HTTP 403: project limit reached", "Free plan allows one project"},
			nil,
		},
		{
			"create 422", false, http.StatusUnprocessableEntity,
			map[string]any{"error": "exceeds the plan's limit of 1 frontend"},
			[]string{"HTTP 422: exceeds the plan's limit of 1 frontend"},
			nil,
		},
		{
			"create 429", false, http.StatusTooManyRequests,
			map[string]any{"error": "too many projects"},
			[]string{"HTTP 429: too many projects", "Try again later"},
			nil,
		},
		{
			"create 503", false, http.StatusServiceUnavailable,
			map[string]any{"error": "GitHub is unavailable"},
			[]string{"HTTP 503: GitHub is unavailable", "Try again shortly"},
			nil,
		},
		{
			"create 409 other", false, http.StatusConflict,
			map[string]any{"error": "something else", "code": "other"},
			[]string{"HTTP 409: something else"},
			[]string{"changed since the preview"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)
			body := map[string]any{"detail": "RAW-DETAIL"}
			maps.Copy(body, tc.body)
			if tc.preview {
				fake.preview = func(int) (int, any) { return tc.status, body }
			} else {
				fake.create = func(int, map[string]any) (int, any) { return tc.status, body }
			}

			_, err := runFrom(t, server, "", "--from", fromLink, "--yes")
			require.Error(t, err)
			assert.Equal(t, tc.status, api.Status(err), "the status stays readable")
			for _, want := range tc.want {
				assert.Contains(t, err.Error(), want)
			}
			for _, absent := range append(tc.absent, "RAW-DETAIL") {
				assert.NotContains(t, err.Error(), absent)
			}
		})
	}
}

func TestProjectShowsTheTemplateSource(t *testing.T) {
	loggedIn(t)
	_, server := newFromServer(t)
	cmd := NewProjects(fromDeps(server))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"get", projectBetaID})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Template source: acme/notes-app@0123456")
}
