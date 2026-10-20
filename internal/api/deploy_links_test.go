package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const deployLinksTestPath = "/projects/22222222-2222-4222-8222-222222222222/deploy-links"

func deployLinkAPIPayload() map[string]any {
	return map[string]any{
		"id":                    "k3Vn8TqPz0aX",
		"project_id":            durableTestProjectID.String(),
		"repo_id":               123456789,
		"repo_full_name":        "acme/notes-app",
		"branch":                "main",
		"path":                  "",
		"suggested_name":        "notes-app",
		"status":                "active",
		"last_validated_at":     "2026-10-19T21:12:00Z",
		"last_validated_commit": "0123456789abcdef0123456789abcdef01234567",
		"created_at":            "2026-10-19T21:12:00Z",
		"updated_at":            "2026-10-19T21:12:00Z",
	}
}

// Only the settings the caller sets are sent, so the API's defaults apply to
// the rest.
func TestCreateDeployLinkSendsOnlySetFields(t *testing.T) {
	branch := "release"
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, deployLinksTestPath, r.URL.Path)
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		writeAPIJSON(t, w, http.StatusCreated, deployLinkAPIPayload())
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	link, err := client.CreateDeployLink(context.Background(), durableTestProjectID,
		apiclient.CreateDeployLinkRequest{Branch: &branch})
	require.NoError(t, err)
	assert.Equal(t, "k3Vn8TqPz0aX", link.Id)
	assert.JSONEq(t, `{"branch":"release"}`, string(body))
}

// Each documented refusal keeps its status and the API's own message.
func TestDeployLinkRefusalsKeepStatusAndMessage(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict,
		http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeAPIJSON(t, w, status, map[string]any{"error": "refused: " + http.StatusText(status)})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			_, err = client.CreateDeployLink(context.Background(), durableTestProjectID, apiclient.CreateDeployLinkRequest{})
			require.Error(t, err)
			assert.Equal(t, status, Status(err))
			assert.Equal(t, "refused: "+http.StatusText(status), Message(err))
		})
	}
}

func TestUpdateDeployLinkPatchesStatus(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, deployLinksTestPath+"/k3Vn8TqPz0aX", r.URL.Path)
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		payload := deployLinkAPIPayload()
		payload["status"] = "disabled"
		writeAPIJSON(t, w, http.StatusOK, payload)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	status := apiclient.UpdateDeployLinkRequestStatusDisabled
	link, err := client.UpdateDeployLink(context.Background(), durableTestProjectID, "k3Vn8TqPz0aX",
		apiclient.UpdateDeployLinkRequest{Status: &status})
	require.NoError(t, err)
	assert.Equal(t, apiclient.DeployLinkStatusDisabled, link.Status)
	assert.JSONEq(t, `{"status":"disabled"}`, string(body))
}

// An unset window is no query parameter, so the API's default applies.
func TestGetDeployLinkStatsSendsDaysOnlyWhenSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		days int
		want url.Values
	}{
		{name: "default window", want: url.Values{}},
		{name: "explicit window", days: 7, want: url.Values{"days": {"7"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, deployLinksTestPath+"/k3Vn8TqPz0aX/stats", r.URL.Path)
				query = r.URL.Query()
				writeAPIJSON(t, w, http.StatusOK, map[string]any{
					"since":  "2026-09-19T21:20:00Z",
					"totals": []any{map[string]any{"kind": "click", "events": 42, "visitors": 31}},
					"daily":  []any{map[string]any{"day": "2026-10-18", "kind": "click", "events": 17}},
				})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			stats, err := client.GetDeployLinkStats(context.Background(), durableTestProjectID, "k3Vn8TqPz0aX", tc.days)
			require.NoError(t, err)
			assert.Equal(t, int64(42), stats.Totals[0].Events)
			assert.Equal(t, tc.want, query)
		})
	}
}

func TestDeleteDeployLinkAcceptsNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	require.NoError(t, client.DeleteDeployLink(context.Background(), durableTestProjectID, "k3Vn8TqPz0aX"))
}
