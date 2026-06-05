package frontends

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsDomainCreate(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	var capturedPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendCommandPayload(frontendID, "web")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedPayload))
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendDomainCommandResponse("app.example.com", "provisioning"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	chainPath := filepath.Join(dir, "chain.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600))
	require.NoError(t, os.WriteFile(chainPath, []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----\n"), 0o600))

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"domain", "create", "web",
		"--domain", "app.example.com",
		"--cert", certPath,
		"--key", keyPath,
		"--chain", chainPath,
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Custom domain 'app.example.com' created for frontend 'web'")
	assert.Contains(t, out, "TLS mode: byoc")
	assert.Contains(t, out, "Domain status: provisioning")

	require.NotNil(t, capturedPayload)
	assert.Equal(t, "app.example.com", capturedPayload["domain"])
	tlsConfig, ok := capturedPayload["tls"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "byoc", tlsConfig["mode"])
	assert.Contains(t, strings.TrimSpace(tlsConfig["certificate_pem"].(string)), "BEGIN CERTIFICATE")
	assert.Contains(t, strings.TrimSpace(tlsConfig["private_key_pem"].(string)), "BEGIN PRIVATE KEY")
	assert.Contains(t, strings.TrimSpace(tlsConfig["certificate_chain_pem"].(string)), "BEGIN CERTIFICATE")
}

func TestFrontendsDomainGet(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendCommandPayload(frontendID, "web")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendDomainCommandResponse("app.example.com", "active"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domain", "get", "web")
	require.NoError(t, err)
	assert.Contains(t, out, "Frontend: web")
	assert.Contains(t, out, "Domain: app.example.com")
	assert.Contains(t, out, "Domain status: active")
	assert.Contains(t, out, "Required routing record:")
	assert.Contains(t, out, "Effective URLs:")
}

func TestFrontendsDomainDeletePromptAndYes(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(frontendDomainDeleteHandler(t, &sawDelete))
		defer server.Close()

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("no\n"))
		out, err := executeFrontendsCommand(t, cmd, "domain", "delete", "web")
		require.NoError(t, err)
		assert.False(t, sawDelete)
		assert.Contains(t, out, "You are about to delete a resource permanently")
		assert.Contains(t, out, "Delete custom domain 'app.example.com on frontend web'?")
		assert.Contains(t, out, "Delete cancelled.")
	})

	t.Run("yes", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
		server := httptest.NewServer(frontendDomainDeleteHandler(t, &sawDelete))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domain", "delete", "web", "--yes")
		require.NoError(t, err)
		assert.True(t, sawDelete)
		assert.Contains(t, out, "Custom domain 'app.example.com' deletion started")
	})
}

func TestFrontendsDomainList(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					frontendCommandPayload(frontendID, "frontend-a"),
					frontendCommandPayload("77777777-7777-4777-8777-777777777777", "frontend-b"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    2,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendDomainCommandResponse("a.example.com", "active"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/77777777-7777-4777-8777-777777777777/domain":
			writeFrontendCommandJSON(t, w, http.StatusNotFound, map[string]string{"error": "custom domain not found"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domain", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "frontend-a")
	assert.Contains(t, out, "a.example.com")
	assert.Contains(t, out, "Total: 1 custom domain(s)")
	assert.NotContains(t, out, "frontend-b")
}

func frontendDomainDeleteHandler(t *testing.T, sawDelete *bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendCommandPayload(frontendID, "web")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendDomainCommandResponse("app.example.com", "active"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			*sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

func frontendDomainCommandResponse(domain, status string) map[string]any {
	return map[string]any{
		"created_at":          "2026-05-20T00:00:00Z",
		"domain":              domain,
		"domain_status":       status,
		"effective_urls":      []string{"https://web.frontends.volcano.dev/", "https://" + domain + "/"},
		"tls_mode":            "byoc",
		"updated_at":          "2026-05-20T00:00:00Z",
		"verification_status": "verified",
		"required_routing_record": map[string]any{
			"name":        domain,
			"record_type": "CNAME",
			"value":       "d123.cloudfront.net",
		},
		"verification_records": []any{
			map[string]any{"type": "TXT", "name": "_volcano." + domain, "value": "verify"},
		},
	}
}
