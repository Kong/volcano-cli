package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.True(t, strings.Contains(err.Error(), "200"), "the status should still be reported: %v", err)
}
