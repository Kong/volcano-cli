package accesstoken

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
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
	}, queries)
}

// A server that keeps saying HasMore while handing back the same page would
// otherwise be walked until the page cap, one request at a time. Nothing new
// arrived, so there is nothing left to find.
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

func TestResolveByNameStopsWhenAPageRepeats(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(true,
			accessTokenTestPayload("33333333-3333-4333-8333-333333333333", "ci-deploy-staging")))
	}))
	defer server.Close()

	_, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.ErrorContains(t, err, `access token "ci-deploy" not found`)
	assert.Equal(t, 2, requests, "the repeated page must end the walk, not extend it")
}

// A server that keeps reporting more, with something new on every page, has no
// natural end. The cap is what stops the CLI hanging on it.
func TestResolveByNameGivesUpAtThePageCap(t *testing.T) {
	setAccessTokenTestHome(t)
	saveAccessTokenTestConfig(t)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeAccessTokenTestJSON(t, w, accessTokenTestPage(true,
			accessTokenTestPayload(fmt.Sprintf("33333333-3333-4333-8333-%012d", requests), "ci-deploy-staging")))
	}))
	defer server.Close()

	_, err := accessTokenTestService(server).Get(context.Background(), "ci-deploy")
	require.ErrorContains(t, err, fmt.Sprintf("gave up looking for access token after %d pages", maxResolvePages))
	assert.Equal(t, maxResolvePages, requests)
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

func writeAccessTokenTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	require.NoError(t, json.NewEncoder(w).Encode(value))
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
