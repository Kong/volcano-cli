package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// The endpoints that mint a credential must never put it in an error.
//
// The generated parser only fills the success field for an exact status and a
// JSON content type, so anything else — a 200 where a 201 was expected, a
// Content-Type stripped by something in the request path — used to fall through
// to the error builder, which surfaced the response body verbatim. That body is
// the plaintext credential.
//
// The failure mode is what makes this worth a test of its own: the user is told
// the call failed, so they have no reason to revoke anything, while a live
// credential sits in stderr and in the CI log that captured it.

const redactionTestSecret = "pt-DO-NOT-LEAK-THIS-VALUE"

func redactionTestProjectID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse("eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550")
	require.NoError(t, err)
	return id
}

// secretBearingServer answers with a created-token body under a status and
// content type the generated parser will not accept.
func secretBearingServer(t *testing.T, status int, contentType, secretField string) *httptest.Server {
	t.Helper()

	body := fmt.Sprintf(`{"id":"77777777-7777-4777-8777-777777777777",`+
		`"name":"ci-deploy","project_id":"eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550",`+
		`"scope":"full","status":"active","token_prefix":"pt-Wq9l2m4X",`+
		`"token_source":"cli","all_time_requests":0,`+
		`"created_at":"2026-09-17T00:00:00Z","%s":%q}`, secretField, redactionTestSecret)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCreateAccessTokenNeverPutsTheSecretInAnError(t *testing.T) {
	// Each of these reaches the error path rather than the success field: the
	// status is unexpected, or the content type is not JSON, or it is missing.
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
	}{
		{"unexpected 2xx", http.StatusOK, "application/json"},
		{"right status, wrong content type", http.StatusCreated, "text/plain"},
		{"right status, no content type", http.StatusCreated, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := secretBearingServer(t, tc.status, tc.contentType, "token")
			client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			token, err := client.CreateAccessToken(context.Background(), redactionTestProjectID(t),
				AccessTokenCreateInput{Name: "ci-deploy", Scope: "full"})
			require.Error(t, err, "an unparseable response must not read as success")
			assert.Nil(t, token)
			assert.NotContains(t, err.Error(), redactionTestSecret,
				"the created token's secret must not reach an error message")
			assert.NotContains(t, err.Error(), "pt-",
				"nor any fragment of it")
		})
	}
}

func TestCreateServiceKeyNeverPutsTheKeyInAnError(t *testing.T) {
	server := secretBearingServer(t, http.StatusOK, "application/json", "key_value")
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	key, err := client.CreateServiceKey(context.Background(), redactionTestProjectID(t), "admin", nil)
	require.Error(t, err)
	assert.Nil(t, key)
	assert.NotContains(t, err.Error(), redactionTestSecret,
		"a created service key's value must not reach an error message")
}

func TestNotificationSettingsNeverPutTheWebhookSecretInAnError(t *testing.T) {
	const secret = "whsec_DO-NOT-LEAK-THIS-VALUE"
	body := `{"project_id":"eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550",` +
		`"email":{"enabled":true,"recipients":[]},` +
		`"webhook":{"url":"https://example.com/hook?token=receiver-token","secret_configured":true,"secret":"` + secret + `"},` +
		`"scheduler_failures":{"notify_on":"first_failure","threshold":3,"notify_on_recovery":true}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	url := "https://example.com/hook"
	_, err = client.UpdateNotificationSettings(context.Background(), redactionTestProjectID(t),
		apiclient.UpdateNotificationSettingsRequest{Webhook: &apiclient.UpdateNotificationWebhook{Url: &url}})
	require.Error(t, err, "an unparseable response must not read as success")
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, err.Error(), "whsec_")

	_, err = client.RotateNotificationWebhookSecret(context.Background(), redactionTestProjectID(t))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "whsec_")

	_, err = client.GetNotificationSettings(context.Background(), redactionTestProjectID(t))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "receiver-token", "the webhook URL can carry the receiver's token")

	_, err = client.SendTestNotification(context.Background(), redactionTestProjectID(t))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "receiver-token")
}

// Dropping the body must not cost a refusal its message or code, whether the API
// declares its status or, like a stale token's 401, does not.
func TestNotificationSettingsKeepTheRefusalMessage(t *testing.T) {
	for _, tt := range []struct {
		status  int
		message string
	}{
		{http.StatusForbidden, "project access token is read-only"},
		{http.StatusUnauthorized, "token expired"},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":"` + tt.message + `","code":"refused"}`))
			}))
			t.Cleanup(server.Close)
			client, err := NewClient(server.URL, "pt-readonly", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			_, err = client.RotateNotificationWebhookSecret(context.Background(), redactionTestProjectID(t))
			require.Error(t, err)
			assert.Equal(t, tt.status, Status(err))
			assert.Equal(t, "refused", Code(err))
			assert.Contains(t, err.Error(), tt.message)

			_, err = client.GetNotificationSettings(context.Background(), redactionTestProjectID(t))
			require.Error(t, err)
			assert.Equal(t, tt.status, Status(err))
			assert.Equal(t, "refused", Code(err))
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}

// The redaction must not cost the caller the status, which is the part they need
// in order to work out what happened.
func TestRedactedErrorsStillCarryTheStatus(t *testing.T) {
	server := secretBearingServer(t, http.StatusOK, "text/plain", "token")
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.CreateAccessToken(context.Background(), redactionTestProjectID(t),
		AccessTokenCreateInput{Name: "ci-deploy", Scope: "full"})
	require.Error(t, err)

	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusOK, apiErr.StatusCode)
	assert.Contains(t, err.Error(), "200", "the status should still be reported")
}
