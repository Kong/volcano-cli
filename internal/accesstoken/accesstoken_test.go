package accesstoken

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const accessTokenTestProjectID = "22222222-2222-4222-8222-222222222222"

// Resolving a name walks the pages the search returns. A project can hold more
// tokens than one page carries, and "ci-deploy" is a substring of plenty of
// other names, so the exact match can sit past the first page.
func TestResolveByNameWalksPages(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	var seen accessTokenTestRequests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.record(r.URL.RawQuery)
		switch r.URL.Query().Get("page") {
		case "1":
			writeAccessTokenTestJSON(t, w, accessTokenTestPage(true,
				accessTokenTestPayload("33333333-3333-4333-8333-333333333333", "ci-deploy-staging")))
		case "2":
			writeAccessTokenTestJSON(t, w, accessTokenTestPage(false,
				accessTokenTestPayload("44444444-4444-4444-8444-444444444444", "ci-deploy")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	token, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.NoError(t, err)
	assert.Equal(t, "44444444-4444-4444-8444-444444444444", token.Id.String())
	assert.Equal(t, []string{
		"page=1&limit=100&search=ci-deploy&include_revoked=true",
		"page=2&limit=100&search=ci-deploy&include_revoked=true",
	}, seen.queries())
}

// After a rotation a project holds two tokens with the same name: the live one
// and the revoked one it replaced, since revoking frees the name. Resolving must
// pick the live one, and must not depend on the API returning it first.
//
// The revoked token is served ahead of the active one here for exactly that
// reason — the API orders newest first today, so a test that mirrored the real
// ordering would pass whether or not the code chose deliberately. Getting this
// wrong means `revoke ci-deploy` revokes the already-dead token, reports
// success, and leaves the live credential working.
func TestResolveByNamePrefersTheUsableToken(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	revoked := accessTokenTestPayload("55555555-5555-4555-8555-555555555555", "ci-deploy")
	revoked["status"] = "revoked"
	active := accessTokenTestPayload("66666666-6666-4666-8666-666666666666", "ci-deploy")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(false, revoked, active))
	}))
	defer server.Close()

	token, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.NoError(t, err)
	assert.Equal(t, "66666666-6666-4666-8666-666666666666", token.Id.String(),
		"resolving a reused name must pick the token that still works")
}

// With no usable match left, the revoked one is still the answer: revoking a
// token twice should report the token rather than deny it exists.
func TestResolveByNameFallsBackToARevokedToken(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	revoked := accessTokenTestPayload("77777777-7777-4777-8777-777777777777", "ci-deploy")
	revoked["status"] = "revoked"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(false, revoked))
	}))
	defer server.Close()

	token, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.NoError(t, err)
	assert.Equal(t, "77777777-7777-4777-8777-777777777777", token.Id.String())
}

// A server that keeps saying HasMore while handing back the same page would
// otherwise be walked until the page cap, one request at a time. Nothing new
// arrived, so there is nothing left to find.
func TestResolveByNameStopsWhenAPageRepeats(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	var seen accessTokenTestRequests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.record(r.URL.RawQuery)
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(true,
			accessTokenTestPayload("33333333-3333-4333-8333-333333333333", "ci-deploy-staging")))
	}))
	defer server.Close()

	_, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.ErrorContains(t, err, `access token "ci-deploy" not found`)
	assert.Len(t, seen.queries(), 2, "the repeated page must end the walk, not extend it")
}

// A server that keeps reporting more, with something new on every page, has no
// natural end. The cap is what stops the CLI hanging on it.
//
// The cap under test is the injected one: proving the walk stops where it is
// told does not need the production thousand round trips.
func TestResolveByNameGivesUpAtThePageCap(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	var seen accessTokenTestRequests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Something new on every page, so nothing but the cap can end the walk.
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(true,
			accessTokenTestPayload(fmt.Sprintf("33333333-3333-4333-8333-%012d", seen.record(r.URL.RawQuery)), "ci-deploy-staging")))
	}))
	defer server.Close()

	service := accessTokenTestService(server)
	service.resolvePageCap = 2

	_, err := service.Get(context.Background(), "ci-deploy")
	require.ErrorContains(t, err, "gave up looking for access token after 2 pages")
	assert.Len(t, seen.queries(), 2)
}

// The default is the production cap, so an injected one cannot quietly become
// the CLI's own limit.
func TestResolvePageCapDefaultsToTheConstant(t *testing.T) {
	assert.Equal(t, maxResolvePages, NewService(cliruntime.Deps{}).pageCap())
}

func accessTokenTestService(server *httptest.Server) Service {
	return NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
}

func setAccessTokenTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveAccessTokenTestConfig(t *testing.T) {
	t.Helper()
	cfg := &config.Config{
		UserToken:      "token",
		CurrentProject: &config.ProjectConfig{ID: accessTokenTestProjectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

// accessTokenTestRequests collects what the server was asked for. The handler
// runs on net/http's goroutine and the assertions on the test's, so nothing
// here is read without the lock.
type accessTokenTestRequests struct {
	mu       sync.Mutex
	recorded []string
}

// record stores one query and returns how many have arrived, which a handler
// needs to vary its answer per page.
func (r *accessTokenTestRequests) record(query string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, query)
	return len(r.recorded)
}

func (r *accessTokenTestRequests) queries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.recorded)
}

// writeAccessTokenTestJSON answers a request from the server's own goroutine,
// where t.FailNow — and so every require helper — is not valid. A failed encode
// is reported instead, and the test fails on its own goroutine.
func writeAccessTokenTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("failed to encode the access token response: %v", err)
	}
}

func accessTokenTestPage(hasMore bool, tokens ...map[string]any) map[string]any {
	return map[string]any{
		"data":     tokens,
		"has_more": hasMore,
		"page":     1,
		"limit":    100,
		"total":    len(tokens),
	}
}

func accessTokenTestPayload(id, name string) map[string]any {
	return map[string]any{
		"all_time_requests": 42,
		"created_at":        time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
		"id":                id,
		"name":              name,
		"project_id":        accessTokenTestProjectID,
		"scope":             "full",
		"status":            "active",
		"token_prefix":      "pt-Wq9l2m4X",
		"token_source":      "cli",
	}
}
