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
	releaseOne = "0b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10"
	releaseTwo = "1b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10"
)

// fromServer is a fake API for `projects create --from`. Its handlers can be
// replaced per test; the counters say what reached it.
type fromServer struct {
	t       *testing.T
	mu      sync.Mutex
	preview func(call int) (int, any)
	create  func(call int, body map[string]any) (int, any)
	get     func(call int) (int, any)
	// retryAfter, when set, is sent with every create's answer.
	retryAfter string

	previews, creates, gets int
	previewAuth             []string
	createAuth              []string
	createKeys              []string
	createBodies            []map[string]any
}

func newFromServer(t *testing.T) (*fromServer, *httptest.Server) {
	t.Helper()
	fake := &fromServer{t: t}
	fake.preview = func(int) (int, any) { return http.StatusOK, previewPayload(releaseOne) }
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusCreated, installingProject("pending", "database", nil)
	}
	fake.get = func(int) (int, any) {
		return http.StatusOK, installingProject("ready", "ready", sourcePayload(releaseOne))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		var status int
		var body any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/share-previews/"+fromLinkID:
			fake.previews++
			fake.previewAuth = append(fake.previewAuth, r.Header.Get("Authorization"))
			status, body = fake.preview(fake.previews)
		case r.Method == http.MethodPost && r.URL.Path == "/forks":
			fake.creates++
			fake.createAuth = append(fake.createAuth, r.Header.Get("Authorization"))
			fake.createKeys = append(fake.createKeys, r.Header.Get("Idempotency-Key"))
			var decoded map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&decoded))
			fake.createBodies = append(fake.createBodies, decoded)
			status, body = fake.create(fake.creates, decoded)
			if fake.retryAfter != "" {
				w.Header().Set("Retry-After", fake.retryAfter)
			}
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

func previewPayload(release string, variables ...map[string]any) map[string]any {
	if variables == nil {
		variables = []map[string]any{{"name": "OPENAI_API_KEY", "required": true, "secret": true}}
	}
	return map[string]any{
		"id":             fromLinkID,
		"suggested_name": "notes-app",
		"release": map[string]any{
			"id":                    release,
			"published_at":          "2026-10-20T09:00:00Z",
			"sha256":                strings.Repeat("ab", 32),
			"frontends":             []string{"web"},
			"functions":             []string{"admin", "api"},
			"service_key_functions": []string{"admin"},
			"variables":             variables,
			"omissions":             []any{map[string]any{"kind": "database", "resource": "main"}},
		},
	}
}

func sourcePayload(release string) map[string]any {
	return map[string]any{"kind": "share_release", "share_id": fromLinkID, "release_id": release}
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

func failedProject(phase string, code any) map[string]any {
	payload := installingProject("failed", phase, nil)
	if code != nil {
		payload["template_installation"].(map[string]any)["failure_code"] = code
	}
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

// interactive makes the environment say a person is at the terminal, so the
// test's stdin answers the prompts, even when the tests run in CI.
func interactive(t *testing.T) {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("VOLCANO_NONINTERACTIVE", "")
	t.Setenv("TERM", "xterm-256color")
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

func createBody(release string, variables map[string]any) map[string]any {
	body := map[string]any{"name": "my-app", "share_id": fromLinkID, "release_id": release}
	if variables != nil {
		body["variables"] = variables
	}
	return body
}

func TestCreateFromShowsTheReleaseAndDeploysItWithNoValues(t *testing.T) {
	loggedIn(t)
	interactive(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "y\n", "--from", fromLink)
	require.NoError(t, err)

	for _, want := range []string{
		"Release: " + releaseOne + " (published ", "Frontends: web", "Functions: admin, api",
		"Variables: OPENAI_API_KEY (starts empty, secret)", "database main: the new project starts without it",
		"These functions receive your new project's service key, which bypasses row-level security: admin.",
		"Fork it?",
		"Project created: my-app (" + projectBetaID + ")", "Installing release " + releaseOne,
		"Project ready: my-app", "Source: share " + fromLinkID + ", release " + releaseOne,
		"volcano projects get " + projectBetaID, "volcano use " + projectBetaID,
		"These variables start empty, so code that reads them may not work until you set them: OPENAI_API_KEY",
		"volcano cloud variables deploy --file <env-file>",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "Now using project", "projects create --from does not change the active project")
	assert.Equal(t, []map[string]any{createBody(releaseOne, nil)}, fake.createBodies,
		"the project is created from the previewed release, with no values")
	require.Len(t, fake.createKeys, 1)
	assert.NotEmpty(t, fake.createKeys[0], "the create carries an idempotency key")
}

// The preview route is public, so it carries no credential; the create does.
func TestCreateFromSendsNoCredentialForThePreview(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=sk-test", "--yes")
	require.NoError(t, err)

	assert.Equal(t, []string{""}, fake.previewAuth)
	assert.Equal(t, []string{"Bearer token"}, fake.createAuth)
}

func TestCreateFromAcceptsABareID(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLinkID, "--var", "OPENAI_API_KEY=x", "--yes")
	require.NoError(t, err)
	assert.Equal(t, 1, fake.creates)
}

func TestCreateFromDeclinedCreatesNothing(t *testing.T) {
	loggedIn(t)
	interactive(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "sk-test\nn\n", "--from", fromLink)
	require.NoError(t, err)
	assert.Zero(t, fake.creates)
	assert.Contains(t, out, "Cancelled.")
}

// A --var fills a variable in; the ones left out start empty, and no value is
// asked for.
func TestCreateFromSendsOnlyTheVarsGiven(t *testing.T) {
	loggedIn(t)
	interactive(t)
	fake, server := newFromServer(t)
	fake.preview = func(int) (int, any) {
		return http.StatusOK, previewPayload(releaseOne,
			map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true},
			map[string]any{"name": "REGION", "required": true, "secret": false},
		)
	}

	out, err := runFrom(t, server, "y\n", "--from", fromLink, "--var", "REGION=eu")
	require.NoError(t, err)

	assert.NotContains(t, out, "OPENAI_API_KEY (secret", "no value is asked for")
	assert.Contains(t, out, "start empty, so code that reads them may not work until you set them: OPENAI_API_KEY\n")
	assert.Equal(t, []map[string]any{createBody(releaseOne, map[string]any{"REGION": "eu"})}, fake.createBodies)
}

func TestCreateFromVarFlagsAreCheckedBeforeAnythingIsCreated(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"not listed", []string{"--var", "OPENAI_API_KEY=x", "--var", "OTHER=y"}, "--var OTHER: the release does not declare this variable; it declares: OPENAI_API_KEY"},
		{"malformed", []string{"--var", "OPENAI_API_KEY"}, `invalid --var "OPENAI_API_KEY": use --var NAME=value`},
		{"no name", []string{"--var", "=x"}, "use --var NAME=value"},
		{"twice", []string{"--var", "OPENAI_API_KEY=x", "--var", "OPENAI_API_KEY=y"}, "--var OPENAI_API_KEY is given more than once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)

			_, err := runFrom(t, server, "", append([]string{"--from", fromLink, "--yes"}, tc.args...)...)
			require.ErrorContains(t, err, tc.want)
			assert.Zero(t, fake.creates)
		})
	}
}

// A value may contain '=' and is sent as given.
func TestCreateFromVarKeepsEverythingAfterTheFirstEquals(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=a=b", "--yes")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{createBody(releaseOne, map[string]any{"OPENAI_API_KEY": "a=b"})}, fake.createBodies)
}

// Where nothing can answer a prompt, a run without --yes fails before it reads
// the link, so it never creates a project unconfirmed.
func TestCreateFromWithoutYesFailsWhereNothingCanAnswer(t *testing.T) {
	loggedIn(t)
	t.Setenv("VOLCANO_NONINTERACTIVE", "1")
	fake, server := newFromServer(t)

	_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x")
	require.ErrorContains(t, err, "needs your confirmation")
	assert.Contains(t, err.Error(), "--yes")
	assert.Zero(t, fake.previews+fake.creates)
}

func TestCreateFromReleaseChangedShowsWhatChangedAndAsksAgainAtATerminal(t *testing.T) {
	loggedIn(t)
	interactive(t)
	fake, server := newFromServer(t)
	fake.preview = func(call int) (int, any) {
		if call == 1 {
			return http.StatusOK, previewPayload(releaseOne)
		}
		return http.StatusOK, previewPayload(releaseTwo,
			map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true},
			map[string]any{"name": "REGION", "required": true, "secret": false},
		)
	}
	fake.create = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{"error": "the link has a new release", "code": "release_changed"}
		}
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(releaseTwo))
	}

	out, err := runFrom(t, server, "y\ny\n", "--from", fromLink, "--var", "OPENAI_API_KEY=sk-test")
	require.NoError(t, err)

	assert.Contains(t, out, "The project's owner published a new release since the preview.")
	assert.Contains(t, out, "was "+releaseOne)
	assert.Contains(t, out, "Variables: +REGION (starts empty)")
	assert.Equal(t, 2, strings.Count(out, "Fork it?"), "the new release is confirmed again")
	require.Len(t, fake.createBodies, 2)
	assert.Equal(t, createBody(releaseTwo, map[string]any{"OPENAI_API_KEY": "sk-test"}), fake.createBodies[1],
		"the second create confirms the new release")
	assert.NotEmpty(t, fake.createKeys[1])
	assert.NotEqual(t, fake.createKeys[0], fake.createKeys[1], "confirming the new release is a new request, with its own key")
}

func TestCreateFromReleaseChangedStopsWhenTheRunAlreadyDecided(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.preview = func(call int) (int, any) {
		if call == 1 {
			return http.StatusOK, previewPayload(releaseOne)
		}
		return http.StatusOK, previewPayload(releaseTwo)
	}
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusConflict, map[string]any{"error": "the link has a new release", "code": "release_changed"}
	}

	out, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
	require.ErrorContains(t, err, "the shared project has a new release since the preview, so nothing was created")
	assert.Contains(t, out, "Release: "+releaseTwo)
	assert.Equal(t, 1, fake.creates, "the new release is never created unseen")
}

// A request whose answer was lost is sent again with the same key, so it
// creates one project at most.
func TestCreateFromRetriesALostAnswerWithTheSameKey(t *testing.T) {
	loggedIn(t)
	var mu sync.Mutex
	var keys []string
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/share-previews/" + fromLinkID:
			writeProjectCommandJSON(t, w, http.StatusOK, previewPayload(releaseOne))
		case "/forks":
			creates++
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			if creates == 1 {
				hijacked, _, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				require.NoError(t, hijacked.Close())
				return
			}
			writeProjectCommandJSON(t, w, http.StatusCreated, installingProject("ready", "ready", sourcePayload(releaseOne)))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.NotEmpty(t, keys[0])
	assert.Equal(t, keys[0], keys[1])
}

// After a lost answer, the resend finds the first attempt still settling; it
// waits as the API says and sends once more with the same key.
func TestCreateFromWaitsWhenTheResendFindsTheFirstAttemptInProgress(t *testing.T) {
	loggedIn(t)
	var mu sync.Mutex
	var keys []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/share-previews/" + fromLinkID:
			writeProjectCommandJSON(t, w, http.StatusOK, previewPayload(releaseOne))
		case "/forks":
			keys = append(keys, r.Header.Get("Idempotency-Key"))
			switch len(keys) {
			case 1:
				hijacked, _, err := w.(http.Hijacker).Hijack()
				require.NoError(t, err)
				require.NoError(t, hijacked.Close())
			case 2:
				w.Header().Set("Retry-After", "0")
				writeProjectCommandJSON(t, w, http.StatusConflict,
					map[string]any{"error": "still settling", "code": "request_in_progress"})
			default:
				writeProjectCommandJSON(t, w, http.StatusCreated, installingProject("ready", "ready", sourcePayload(releaseOne)))
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
	require.NoError(t, err)
	require.Len(t, keys, 3)
	assert.NotEmpty(t, keys[0])
	assert.Equal(t, []string{keys[0], keys[0], keys[0]}, keys, "one request keeps its key")
}

func TestCreateFromWaitsThroughThePhasesToReady(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	phases := []string{"restore", "configure", "deploy"}
	fake.get = func(call int) (int, any) {
		if call <= len(phases) {
			return http.StatusOK, installingProject("running", phases[call-1], nil)
		}
		return http.StatusOK, installingProject("ready", "ready", sourcePayload(releaseOne))
	}

	out, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
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

// A failed installation says why in words: a known code plainly, an unknown
// one generically with the code, and a missing one as no recorded reason.
func TestCreateFromFailedInstallationSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		code any
		want string
	}{
		{"source_withdrawn", "the share was switched off or deleted"},
		{"source_invalid", "the release failed Volcano's checks"},
		{"something_new", "it failed (something_new)"},
		{nil, "no reason was recorded"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)
			fake.get = func(int) (int, any) { return http.StatusOK, failedProject("deploy", tc.code) }

			out, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
			require.Error(t, err)
			assert.Contains(t, out, "installation failed during deploy: "+tc.want)
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "The project is kept")
			assert.Contains(t, err.Error(), "volcano projects get "+projectBetaID)
			assert.Contains(t, err.Error(), "volcano projects delete "+projectBetaID)
			assert.NotContains(t, out, "Project ready")
		})
	}
}

// A 201 without an installation means nothing is being installed, so there
// is nothing to wait for and the run does not report success.
func TestCreateFromWithoutAnInstallationSaysNothingIsInstalled(t *testing.T) {
	for _, extra := range [][]string{nil, {"--no-wait"}} {
		loggedIn(t)
		fake, server := newFromServer(t)
		fake.create = func(int, map[string]any) (int, any) {
			return http.StatusCreated, projectCommandPayload(projectBetaID, "my-app", "active", nil)
		}

		out, err := runFrom(t, server, "", append([]string{"--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes"}, extra...)...)
		require.ErrorContains(t, err, "the release is not being installed in it, so treat it as not installed")
		assert.Contains(t, out, "Project created: my-app")
		assert.Zero(t, fake.gets)
		assert.NotContains(t, out, "Project ready")
	}
}

func TestCreateFromReadyInTheCreateResponseNeedsNoPolling(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(releaseOne))
	}

	out, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
	require.NoError(t, err)
	assert.Zero(t, fake.gets)
	assert.Contains(t, out, "Project ready")
}

func TestCreateFromNoWaitReturnsRightAfterCreation(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)

	out, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes", "--no-wait")
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
	cmd.SetArgs([]string{"create", "my-app", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes"})
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
		{[]string{"--var", "A=b"}, "--var only applies with --from"},
		{[]string{"--yes"}, "--yes only applies with --from"},
		{[]string{"--no-wait"}, "--no-wait only applies with --from"},
		{[]string{"--from", fromLink, "--commit", "0123456"}, "unknown flag: --commit"},
		{[]string{"--from", "https://volcano.dev/projects/" + fromLinkID}, "invalid share link"},
		{[]string{"--from", "short"}, "invalid share link"},
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
		name       string
		preview    bool
		status     int
		body       map[string]any
		retryAfter string
		want       []string
		absent     []string
	}{
		{
			name: "preview 404", preview: true, status: http.StatusNotFound,
			body:   map[string]any{"error": "SECRET reason: the owner turned it off"},
			want:   []string{"This share link is not available.", "has no published release"},
			absent: []string{"SECRET", "owner"},
		},
		{
			name: "preview gate", preview: true, status: http.StatusNotFound,
			body:   map[string]any{"error": "SECRET gate", "code": "feature_unavailable"},
			want:   []string{"Sharing projects is not available in this environment."},
			absent: []string{"SECRET"},
		},
		{
			name: "preview 429", preview: true, status: http.StatusTooManyRequests,
			body: map[string]any{"error": "slow down"},
			want: []string{"HTTP 429: slow down", "Try again shortly"},
		},
		{
			name: "preview proxy credential", preview: true, status: http.StatusForbidden,
			body: map[string]any{"error": "bad proof", "code": "proxy_credential_rejected"},
			want: []string{"HTTP 403: bad proof", "not about your session"},
		},
		{
			name: "create 404", status: http.StatusNotFound,
			body:   map[string]any{"error": "SECRET gone"},
			want:   []string{"This share link is not available."},
			absent: []string{"SECRET"},
		},
		{
			name: "create gate", status: http.StatusBadRequest,
			body:   map[string]any{"error": "SECRET gate", "code": "feature_unavailable"},
			want:   []string{"Sharing projects is not available in this environment."},
			absent: []string{"SECRET"},
		},
		{
			name: "create local mode", status: http.StatusBadRequest,
			body: map[string]any{"error": "project templates require cloud mode"},
			want: []string{"HTTP 400: project templates require cloud mode"},
		},
		{
			name: "plan limit", status: http.StatusForbidden,
			body: map[string]any{"error": "the release exceeds the plan's limit of 1 frontend"},
			want: []string{"HTTP 403: the release exceeds the plan's limit of 1 frontend", "Your plan does not allow what this release deploys"},
		},
		{
			name: "variables required", status: http.StatusUnprocessableEntity,
			body: map[string]any{"error": "OPENAI_API_KEY needs a value", "code": "variables_required"},
			want: []string{"HTTP 422: OPENAI_API_KEY needs a value", "ask the person who shared it to share it again"},
		},
		{
			name: "variable not declared", status: http.StatusUnprocessableEntity,
			body: map[string]any{"error": "OTHER is not declared", "code": "variable_not_declared"},
			want: []string{"HTTP 422: OTHER is not declared", "only for the variables the preview lists"},
		},
		{
			name: "release cannot be installed", status: http.StatusUnprocessableEntity,
			body: map[string]any{"error": "the release cannot be installed"},
			want: []string{"HTTP 422: the release cannot be installed"},
		},
		{
			name: "key reused", status: http.StatusConflict,
			body: map[string]any{"error": "key reused", "code": "idempotency_key_reused"},
			want: []string{"HTTP 409: key reused", "already used for a different request", "Run the command again"},
		},
		{
			name: "create 429", status: http.StatusTooManyRequests, retryAfter: "1200",
			body: map[string]any{"error": "too many projects"},
			want: []string{"HTTP 429: too many projects", "nothing was created", "Try again in 20m0s."},
		},
		{
			name: "rate limit unavailable", status: http.StatusServiceUnavailable, retryAfter: "5",
			body: map[string]any{"error": "cannot count", "code": "rate_limit_unavailable"},
			want: []string{"HTTP 503: cannot count", "Volcano cannot count forks right now", "Try again in 5s."},
		},
		{
			name: "durable paused", status: http.StatusServiceUnavailable, retryAfter: "60",
			body: map[string]any{"error": "paused", "code": "feature_paused"},
			want: []string{"Durable functions are paused", "Try again in 1m0s."},
		},
		{
			name: "unavailable without a wait", status: http.StatusServiceUnavailable,
			body: map[string]any{"error": "unavailable", "code": "service_unavailable"},
			want: []string{"HTTP 503: unavailable", "Try again shortly."},
		},
		{
			name: "create 451", status: http.StatusUnavailableForLegalReasons,
			body: map[string]any{"error": "sanctions"},
			want: []string{"HTTP 451: sanctions", "under US sanctions"},
		},
		{
			name: "create 409 other", status: http.StatusConflict,
			body:   map[string]any{"error": "something else", "code": "other"},
			want:   []string{"HTTP 409: something else"},
			absent: []string{"new release"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loggedIn(t)
			fake, server := newFromServer(t)
			fake.retryAfter = tc.retryAfter
			body := map[string]any{"detail": "RAW-DETAIL"}
			maps.Copy(body, tc.body)
			if tc.preview {
				fake.preview = func(int) (int, any) { return tc.status, body }
			} else {
				fake.create = func(int, map[string]any) (int, any) { return tc.status, body }
			}

			_, err := runFrom(t, server, "", "--from", fromLink, "--var", "OPENAI_API_KEY=x", "--yes")
			require.Error(t, err)
			assert.Equal(t, tc.status, api.Status(err), "the status stays readable")
			for _, want := range tc.want {
				assert.Contains(t, err.Error(), want)
			}
			for _, absent := range append(tc.absent, "RAW-DETAIL") {
				assert.NotContains(t, err.Error(), absent)
			}
			if !tc.preview {
				assert.Equal(t, 1, fake.creates, "a refusal is not retried")
			}
		})
	}
}

func TestProjectShowsTheTemplateSourceAndWhyItFailed(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	run := func() string {
		cmd := NewProjects(fromDeps(server))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"get", projectBetaID})
		require.NoError(t, cmd.Execute())
		return out.String()
	}

	assert.Contains(t, run(), "Template source: share "+fromLinkID+", release "+releaseOne)

	fake.get = func(int) (int, any) { return http.StatusOK, failedProject("deploy", "source_withdrawn") }
	assert.Contains(t, run(), "Template failure: the share was switched off or deleted")
}

// An earlier attempt still settling is asked about once more with the same
// key, after the wait the API names, and answers with the project it made.
func TestCreateFromWaitsForAnAttemptInProgress(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.retryAfter = "0"
	fake.create = func(call int, _ map[string]any) (int, any) {
		if call == 1 {
			return http.StatusConflict, map[string]any{"error": "still settling", "code": "request_in_progress"}
		}
		return http.StatusCreated, installingProject("ready", "ready", sourcePayload(releaseOne))
	}

	_, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.NoError(t, err)
	require.Equal(t, 2, fake.creates)
	assert.NotEmpty(t, fake.createKeys[0])
	assert.Equal(t, fake.createKeys[0], fake.createKeys[1], "the same confirmation keeps its key")
}

// An attempt that is still settling after the wait is reported, not retried
// again, and says not to start a second copy blindly.
func TestCreateFromReportsAnAttemptStillInProgress(t *testing.T) {
	loggedIn(t)
	fake, server := newFromServer(t)
	fake.retryAfter = "0"
	fake.create = func(int, map[string]any) (int, any) {
		return http.StatusConflict, map[string]any{"error": "still settling", "code": "request_in_progress"}
	}

	_, err := runFrom(t, server, "", "--from", fromLink, "--yes")
	require.ErrorContains(t, err, "An earlier attempt of this fork is still being set up")
	assert.Contains(t, err.Error(), "volcano projects list")
	assert.Equal(t, 2, fake.creates)
}
