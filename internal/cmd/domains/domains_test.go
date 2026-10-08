package domains

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const ownershipValue = "volcano-domain-verification=6f0d3c1b9e2a47d58c4b1a0e9f3d2c7b"

func executeDomainsCommand(t *testing.T, cmd *cobra.Command, in string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(in))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setDomainsTestConfig(t *testing.T, token string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	require.NoError(t, (&cliconfig.Config{UserToken: token}).Save())
}

// writeDomainsJSON answers from the server's goroutine, where require is not
// valid, so a failed encode is reported instead.
func writeDomainsJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode the domains response: %v", err)
	}
}

func verifiedDomainPayload(domain string) map[string]any {
	return map[string]any{"domain": domain, "verified_at": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)}
}

func domainsServer(t *testing.T, handler http.HandlerFunc) cliruntime.Deps {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

func TestDomainsListShowsTheAccountsDomains(t *testing.T) {
	setDomainsTestConfig(t, "token")
	deps := domainsServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/user/domains", r.URL.Path)
		writeDomainsJSON(t, w, http.StatusOK, map[string]any{"domains": []any{verifiedDomainPayload("example.com")}})
	})

	out, err := executeDomainsCommand(t, New(deps), "", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "example.com")
	assert.Contains(t, out, "2h ago")
	assert.Contains(t, out, "Total: 1 verified domain(s)")

	out, err = executeDomainsCommand(t, New(deps), "", "list", "--json")
	require.NoError(t, err)
	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "example.com", listed[0]["domain"])
}

func TestDomainsListWithoutDomains(t *testing.T) {
	setDomainsTestConfig(t, "token")
	deps := domainsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeDomainsJSON(t, w, http.StatusOK, map[string]any{"domains": []any{}})
	})

	out, err := executeDomainsCommand(t, New(deps), "", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No verified domains")
}

func TestDomainsVerifyNamesTheRecordToPublish(t *testing.T) {
	setDomainsTestConfig(t, "token")
	deps := domainsServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode the verify request: %v", err)
		}
		assert.Equal(t, map[string]any{"domain": "example.com"}, body)
		writeDomainsJSON(t, w, http.StatusConflict, map[string]any{
			"error": "ownership of example.com is not verified",
			"code":  api.OwnershipVerificationRequired,
			"required_record": map[string]any{
				"name": "_volcano.example.com", "type": "TXT", "value": ownershipValue,
			},
		})
	})

	_, err := executeDomainsCommand(t, New(deps), "", "verify", "example.com")
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, api.Status(err))
	assert.Contains(t, err.Error(), "ownership of example.com is not verified")
	assert.Contains(t, err.Error(), "Publish this DNS record, then run the command again:")
	assert.Contains(t, err.Error(), `_volcano.example.com  TXT  "`+ownershipValue+`"`)
	ownership, ok := errors.AsType[*api.OwnershipRequiredError](err)
	require.True(t, ok)
	assert.Equal(t, ownershipValue, ownership.Record.Value)
}

func TestDomainsVerifyReportsANewAndAnExistingDomain(t *testing.T) {
	setDomainsTestConfig(t, "token")
	status := http.StatusCreated
	deps := domainsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeDomainsJSON(t, w, status, verifiedDomainPayload("example.com"))
	})

	out, err := executeDomainsCommand(t, New(deps), "", "verify", "example.com")
	require.NoError(t, err)
	assert.Contains(t, out, "Domain 'example.com' verified")
	assert.Contains(t, out, "Keep the TXT record published")

	status = http.StatusOK
	out, err = executeDomainsCommand(t, New(deps), "", "verify", "example.com")
	require.NoError(t, err)
	assert.Contains(t, out, "Domain 'example.com' is already verified")
}

func TestDomainsVerifyReportsAnotherAccountsDomain(t *testing.T) {
	setDomainsTestConfig(t, "token")
	deps := domainsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeDomainsJSON(t, w, http.StatusConflict, map[string]any{
			"error": "domain is verified by another account whose TXT record is still published",
		})
	})

	_, err := executeDomainsCommand(t, New(deps), "", "verify", "example.com")
	require.Error(t, err)
	assert.Equal(t, "HTTP 409: domain is verified by another account whose TXT record is still published", err.Error())
}

func TestDomainsRemove(t *testing.T) {
	setDomainsTestConfig(t, "token")
	var removed []string
	deps := domainsServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		removed = append(removed, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/missing.example.com") {
			writeDomainsJSON(t, w, http.StatusNotFound, map[string]any{"error": "verified domain not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := executeDomainsCommand(t, New(deps), "", "remove", "example.com", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Domain 'example.com' removed")

	out, err = executeDomainsCommand(t, New(deps), "y\n", "remove", "example.com")
	require.NoError(t, err)
	assert.Contains(t, out, "Remove verified domain 'example.com'?")

	_, err = executeDomainsCommand(t, New(deps), "", "remove", "missing.example.com", "--yes")
	require.Error(t, err)
	assert.Equal(t, "HTTP 404: verified domain not found", err.Error())
	assert.Equal(t, []string{"/user/domains/example.com", "/user/domains/example.com", "/user/domains/missing.example.com"}, removed)
}

func TestDomainsRemoveDeclinedSendsNothing(t *testing.T) {
	setDomainsTestConfig(t, "token")
	deps := domainsServer(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})

	_, err := executeDomainsCommand(t, New(deps), "n\n", "remove", "example.com")
	require.NoError(t, err)
}

func TestDomainsRefuseAProjectAccessToken(t *testing.T) {
	setDomainsTestConfig(t, cliconfig.ProjectTokenPrefix+"fake")
	deps := domainsServer(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})

	_, err := executeDomainsCommand(t, New(deps), "", "list")
	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
}

func TestDomainsIsACloudCommand(t *testing.T) {
	_, err := executeDomainsCommand(t, NewCloudOnly(), "", "verify", "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"domains" is a cloud command`)
	assert.Contains(t, err.Error(), "volcano cloud domains")

	out, err := executeDomainsCommand(t, NewCloudOnly(), "", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "this command is cloud-only")
}
