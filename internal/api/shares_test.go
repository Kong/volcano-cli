package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const testShareID = "k3Vn8TqPz0aX"

var testReleaseID = uuid.MustParse("0b6c1f1e-4a55-4d6f-9c3e-5d2f7a1b9e10")

func shareAPIPayload() map[string]any {
	return map[string]any{
		"id":              testShareID,
		"project_id":      durableTestProjectID.String(),
		"suggested_name":  "notes-app",
		"description":     "",
		"allow_reshare":   false,
		"status":          "active",
		"serving":         false,
		"current_release": nil,
		"created_at":      "2026-10-19T21:12:00Z",
		"updated_at":      "2026-10-19T21:12:00Z",
	}
}

func newShareTestClient(t *testing.T, token string, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, token, WithHTTPClient(server.Client()))
	require.NoError(t, err)
	return client
}

// The project is named in the query, and its shares come back as a list.
func TestListSharesNamesTheProject(t *testing.T) {
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/shares", r.URL.Path)
		assert.Equal(t, url.Values{"project_id": {durableTestProjectID.String()}}, r.URL.Query())
		writeAPIJSON(t, w, http.StatusOK, map[string]any{"shares": []any{shareAPIPayload()}})
	})

	shares, err := client.ListShares(context.Background(), durableTestProjectID)
	require.NoError(t, err)
	require.Len(t, shares, 1)
	assert.Equal(t, testShareID, shares[0].Id)
	assert.False(t, shares[0].Serving)
}

// Only the settings the caller sets are sent, with the project, so the API's
// defaults apply to the rest.
func TestCreateShareSendsTheProjectAndOnlySetFields(t *testing.T) {
	name := "notes-app"
	var bodies []string
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/shares", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		bodies = append(bodies, string(body))
		writeAPIJSON(t, w, http.StatusCreated, shareAPIPayload())
	})

	share, err := client.CreateShare(context.Background(),
		apiclient.ShareCreateRequest{ProjectId: durableTestProjectID, SuggestedName: &name})
	require.NoError(t, err)
	assert.Equal(t, testShareID, share.Id)
	assert.Nil(t, share.CurrentRelease, "a new share is a draft")
	_, err = client.CreateShare(context.Background(), apiclient.ShareCreateRequest{ProjectId: durableTestProjectID})
	require.NoError(t, err)
	require.Len(t, bodies, 2)
	assert.JSONEq(t, `{"project_id":"`+durableTestProjectID.String()+`","suggested_name":"notes-app"}`, bodies[0])
	assert.JSONEq(t, `{"project_id":"`+durableTestProjectID.String()+`"}`, bodies[1])
}

// Each documented refusal keeps its status, code and the API's own message.
func TestShareRefusalsKeepStatusCodeAndMessage(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, ""},
		{http.StatusUnauthorized, ""},
		{http.StatusForbidden, "operator_disabled"},
		{http.StatusForbidden, "source_unavailable"},
		{http.StatusForbidden, "reshare_not_allowed"},
		{http.StatusNotFound, "feature_unavailable"},
		{http.StatusConflict, ""},
		{http.StatusInternalServerError, ""},
	} {
		t.Run(http.StatusText(tc.status)+" "+tc.code, func(t *testing.T) {
			client := newShareTestClient(t, "token", func(w http.ResponseWriter, _ *http.Request) {
				payload := map[string]any{"error": "refused: " + http.StatusText(tc.status)}
				if tc.code != "" {
					payload["code"] = tc.code
				}
				writeAPIJSON(t, w, tc.status, payload)
			})

			_, err := client.CreateShare(context.Background(), apiclient.ShareCreateRequest{ProjectId: durableTestProjectID})
			require.Error(t, err)
			assert.Equal(t, tc.status, Status(err))
			assert.Equal(t, tc.code, Code(err))
			assert.Equal(t, "refused: "+http.StatusText(tc.status), Message(err))
		})
	}
}

// Reading or changing one share names it in the path and its project in the
// query, so a share of another project answers like an unknown one.
func TestGetAndUpdateShareNameTheShareAndProject(t *testing.T) {
	var requests []string
	var body []byte
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		payload := shareAPIPayload()
		if r.Method == http.MethodPatch {
			body, _ = io.ReadAll(r.Body)
			payload["status"] = "disabled"
			payload["disabled_reason"] = "owner"
		}
		writeAPIJSON(t, w, http.StatusOK, payload)
	})

	_, err := client.GetShare(context.Background(), durableTestProjectID, testShareID)
	require.NoError(t, err)
	status := apiclient.ShareUpdateRequestStatusDisabled
	share, err := client.UpdateShare(context.Background(), durableTestProjectID, testShareID,
		apiclient.ShareUpdateRequest{Status: &status})
	require.NoError(t, err)
	assert.Equal(t, apiclient.ShareStatusDisabled, share.Status)
	require.NotNil(t, share.DisabledReason)
	assert.Equal(t, "owner", *share.DisabledReason)
	assert.JSONEq(t, `{"status":"disabled"}`, string(body))
	query := "project_id=" + durableTestProjectID.String()
	assert.Equal(t, []string{
		"GET /shares/" + testShareID + "?" + query,
		"PATCH /shares/" + testShareID + "?" + query,
	}, requests)
}

// An edit the API dropped while it switched the share off keeps its code.
func TestUpdateShareKeepsEditDropped(t *testing.T) {
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusBadRequest, map[string]any{"error": "the edit was not saved", "code": "edit_dropped"})
	})
	status := apiclient.ShareUpdateRequestStatusDisabled
	description := "new"
	_, err := client.UpdateShare(context.Background(), durableTestProjectID, testShareID,
		apiclient.ShareUpdateRequest{Status: &status, Description: &description})
	require.Error(t, err)
	assert.Equal(t, "edit_dropped", Code(err))
}

// An unset window is no query parameter, so the API's default applies.
func TestGetShareStatsSendsDaysOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		days int
		want string
	}{
		{name: "default window"},
		{name: "explicit window", days: 7, want: "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/share-stats", r.URL.Path)
				assert.Equal(t, testShareID, r.URL.Query().Get("share_id"))
				assert.Equal(t, durableTestProjectID.String(), r.URL.Query().Get("project_id"))
				assert.Equal(t, tc.want, r.URL.Query().Get("days"))
				writeAPIJSON(t, w, http.StatusOK, map[string]any{
					"since": "2026-09-19T00:00:00Z", "totals": []any{}, "daily": []any{},
				})
			})
			_, err := client.GetShareStats(context.Background(), durableTestProjectID, testShareID, tc.days)
			require.NoError(t, err)
		})
	}
}

func previewAPIPayload() map[string]any {
	return map[string]any{
		"id":             testShareID,
		"suggested_name": "notes-app",
		"description":    "Notes that sync.",
		"release": map[string]any{
			"id":                    testReleaseID.String(),
			"published_at":          "2026-10-20T09:00:00Z",
			"sha256":                "9f2c4c0e7d8a1b3f5e6d7c8b9a0f1e2d3c4b5a69788796a5b4c3d2e1f0a9b8c7",
			"frontends":             []string{"web"},
			"functions":             []string{"admin", "api"},
			"service_key_functions": []string{"admin"},
			"variables":             []any{map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true}},
			"omissions":             []any{map[string]any{"kind": "database", "resource": "main"}},
		},
	}
}

func TestGetSharePreviewSendsNoCredentialWithoutAToken(t *testing.T) {
	var auth []string
	client := newShareTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/share-previews/"+testShareID, r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		auth = r.Header.Values("Authorization")
		writeAPIJSON(t, w, http.StatusOK, previewAPIPayload())
	})

	preview, err := client.GetSharePreview(context.Background(), testShareID)
	require.NoError(t, err)
	assert.Empty(t, auth)
	assert.Equal(t, testReleaseID, preview.Release.Id)
	assert.Equal(t, []string{"admin"}, preview.Release.ServiceKeyFunctions)
}

// A preview limited per address says when to try again.
func TestGetSharePreviewKeepsRetryAfter(t *testing.T) {
	client := newShareTestClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		writeAPIJSON(t, w, http.StatusTooManyRequests, map[string]any{"error": "too many previews"})
	})

	_, err := client.GetSharePreview(context.Background(), testShareID)
	require.Error(t, err)
	now := time.Now()
	at, ok := RetryAt(err, now)
	require.True(t, ok)
	assert.Equal(t, now.Add(120*time.Second), at)
	assert.Equal(t, http.StatusTooManyRequests, Status(err))
}

func TestCreateForkSendsTheShareReleaseVariablesAndKey(t *testing.T) {
	var body []byte
	var key string
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/forks", r.URL.Path)
		key = r.Header.Get("Idempotency-Key")
		body, _ = io.ReadAll(r.Body)
		writeAPIJSON(t, w, http.StatusConflict, map[string]any{"error": "the share has a new release", "code": "release_changed"})
	})

	_, err := client.CreateFork(context.Background(), ForkRequest{
		Name: " my-app ", ShareID: testShareID, ReleaseID: testReleaseID,
		Variables:      map[string]string{"OPENAI_API_KEY": "sk-test"},
		IdempotencyKey: "key-1",
	})
	require.Error(t, err)
	assert.JSONEq(t, `{"name":"my-app","share_id":"k3Vn8TqPz0aX","release_id":"`+testReleaseID.String()+
		`","variables":{"OPENAI_API_KEY":"sk-test"}}`, string(body))
	assert.Equal(t, "key-1", key)
	assert.Equal(t, http.StatusConflict, Status(err))
	assert.Equal(t, "release_changed", Code(err))
}

// A fork that gives no values sends no variables, and no key sends no header.
// A refusal that says when to try again keeps it.
func TestCreateForkOmitsWhatIsNotSetAndKeepsRetryAfter(t *testing.T) {
	var body []byte
	var keys []string
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		keys = r.Header.Values("Idempotency-Key")
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Retry-After", "5")
		writeAPIJSON(t, w, http.StatusServiceUnavailable, map[string]any{"error": "cannot count", "code": "rate_limit_unavailable"})
	})

	_, err := client.CreateFork(context.Background(), ForkRequest{Name: "my-app", ShareID: testShareID, ReleaseID: testReleaseID})
	require.Error(t, err)
	assert.Empty(t, keys)
	assert.JSONEq(t, `{"name":"my-app","share_id":"k3Vn8TqPz0aX","release_id":"`+testReleaseID.String()+`"}`, string(body))
	assert.Equal(t, "rate_limit_unavailable", Code(err))
	_, ok := RetryAt(err, time.Now())
	assert.True(t, ok)
}

func draftAPIPayload(draftID string, serviceKey ...string) map[string]any {
	if serviceKey == nil {
		serviceKey = []string{}
	}
	return map[string]any{
		"draft_id":              draftID,
		"functions":             []any{map[string]any{"name": "admin", "kind": "standard", "visibility": "private"}},
		"frontends":             []string{"web"},
		"variables":             []any{map[string]any{"name": "OPENAI_API_KEY", "required": true, "secret": true}},
		"service_key_functions": serviceKey,
		"omissions":             []any{},
		"frontend_variables":    map[string]any{"web": []string{}},
	}
}

func TestPublishShareReleaseSendsTheConfirmationAndKeepsTheNewDraft(t *testing.T) {
	var body []byte
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/share-releases", r.URL.Path)
		body, _ = io.ReadAll(r.Body)
		writeAPIJSON(t, w, http.StatusConflict, map[string]any{
			"error": "the project changed", "code": "draft_changed", "draft": draftAPIPayload("d2", "admin", "worker"),
		})
	})

	_, err := client.PublishShareRelease(context.Background(), apiclient.ShareReleaseCreateRequest{
		ShareId: testShareID, DraftId: "d1", Variables: []string{"OPENAI_API_KEY"}, ServiceKeyFunctions: []string{"admin"},
	})
	require.Error(t, err)
	assert.JSONEq(t, `{"share_id":"k3Vn8TqPz0aX","draft_id":"d1","variables":["OPENAI_API_KEY"],`+
		`"service_key_functions":["admin"]}`, string(body))
	changed, ok := errors.AsType[*DraftChangedError](err)
	require.True(t, ok)
	assert.Equal(t, "d2", changed.Draft.DraftId)
	assert.Equal(t, []string{"admin", "worker"}, changed.Draft.ServiceKeyFunctions)
	assert.Equal(t, http.StatusConflict, Status(err))
	assert.Equal(t, DraftChangedCode, Code(err))
}

// A 409 with another code is a plain refusal, not a changed draft, and a
// refusal that says when to try again keeps it.
func TestPublishShareReleaseOtherRefusalsArePlainErrors(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code       string
		retryAfter string
	}{
		{http.StatusConflict, "something_else", ""},
		{http.StatusTooManyRequests, "release_builds_limited", "600"},
		{http.StatusServiceUnavailable, "builds_busy", "10"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			client := newShareTestClient(t, "token", func(w http.ResponseWriter, _ *http.Request) {
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				writeAPIJSON(t, w, tc.status, map[string]any{"error": "refused", "code": tc.code})
			})

			_, err := client.PublishShareRelease(context.Background(), apiclient.ShareReleaseCreateRequest{
				ShareId: testShareID, DraftId: "d1", Variables: []string{}, ServiceKeyFunctions: []string{},
			})
			require.Error(t, err)
			_, changed := errors.AsType[*DraftChangedError](err)
			assert.False(t, changed)
			assert.Equal(t, tc.status, Status(err))
			assert.Equal(t, tc.code, Code(err))
			assert.Equal(t, "refused", Message(err))
			_, ok := RetryAt(err, time.Now())
			assert.Equal(t, tc.retryAfter != "", ok)
		})
	}
}

func TestGetShareReleaseDraftReadsTheDraftID(t *testing.T) {
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/share-release-drafts", r.URL.Path)
		assert.Equal(t, testShareID, r.URL.Query().Get("share_id"))
		assert.Equal(t, durableTestProjectID.String(), r.URL.Query().Get("project_id"))
		writeAPIJSON(t, w, http.StatusOK, draftAPIPayload("d1", "admin"))
	})

	draft, err := client.GetShareReleaseDraft(context.Background(), durableTestProjectID, testShareID)
	require.NoError(t, err)
	assert.Equal(t, "d1", draft.DraftId)
	assert.Equal(t, []string{"admin"}, draft.ServiceKeyFunctions)
}

// A page names its size and cursor, and a next cursor is kept only while there
// is more.
func TestListShareReleasesPages(t *testing.T) {
	var queries []url.Values
	client := newShareTestClient(t, "token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/share-releases", r.URL.Path)
		queries = append(queries, r.URL.Query())
		if r.URL.Query().Get("cursor") == "" {
			writeAPIJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "limit": 2, "has_more": true, "next_cursor": "c2"})
			return
		}
		writeAPIJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "limit": 2, "has_more": false, "next_cursor": "stale"})
	})

	first, err := client.ListShareReleases(context.Background(), durableTestProjectID, testShareID, 2, "")
	require.NoError(t, err)
	assert.Equal(t, "c2", first.NextCursor)
	last, err := client.ListShareReleases(context.Background(), durableTestProjectID, testShareID, 2, first.NextCursor)
	require.NoError(t, err)
	assert.Empty(t, last.NextCursor, "no next page once has_more is false")
	require.Len(t, queries, 2)
	assert.Equal(t, "2", queries[0].Get("limit"))
	assert.Empty(t, queries[0].Get("cursor"))
	assert.Equal(t, "c2", queries[1].Get("cursor"))
	assert.Equal(t, testShareID, queries[1].Get("share_id"))
}
