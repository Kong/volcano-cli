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
	capturedPayload := map[string]any{}
	server := httptest.NewServer(frontendDomainCreateHandler(t, nil, http.StatusCreated,
		frontendDomainCommandResponse("app.example.com", "provisioning"), capturedPayload))
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
	assert.Contains(t, out, "DNS routing target: web.frontends.volcano.run")

	require.NotEmpty(t, capturedPayload)
	assert.Equal(t, "app.example.com", capturedPayload["domain"])
	tlsConfig, ok := capturedPayload["tls"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "byoc", tlsConfig["mode"])
	assert.Contains(t, strings.TrimSpace(tlsConfig["certificate_pem"].(string)), "BEGIN CERTIFICATE")
	assert.Contains(t, strings.TrimSpace(tlsConfig["private_key_pem"].(string)), "BEGIN PRIVATE KEY")
	assert.Contains(t, strings.TrimSpace(tlsConfig["certificate_chain_pem"].(string)), "BEGIN CERTIFICATE")
}

func TestFrontendsDomainCreateNamesTheOwnershipRecord(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{frontendCommandPayload(frontendID, "web")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			writeFrontendCommandJSON(t, w, http.StatusConflict, map[string]any{
				"error": "ownership of example.com is not verified",
				"code":  "ownership_verification_required",
				"required_record": map[string]any{
					"name": "_volcano.example.com", "type": "TXT", "value": "volcano-domain-verification=abc",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600))

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"domain", "create", "web", "--domain", "app.example.com", "--cert", certPath, "--key", keyPath,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 409: ownership of example.com is not verified")
	assert.Contains(t, err.Error(), `_volcano.example.com  TXT  "volcano-domain-verification=abc"`)
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
	assert.NotContains(t, out, "Required routing record:")
	assert.Contains(t, out, "DNS routing target: web.frontends.volcano.run")
	assert.Contains(t, out, "Use CNAME only if your DNS provider confirms this is not a zone apex")
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
					frontendCommandPayload("88888888-8888-4888-8888-888888888888", "frontend-c"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    3,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendDomainCommandResponse("a.example.com", "active"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/77777777-7777-4777-8777-777777777777/domain":
			writeFrontendCommandJSON(t, w, http.StatusOK, nil)
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/88888888-8888-4888-8888-888888888888/domain":
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
	assert.NotContains(t, out, "frontend-c")
}

func TestFrontendsDomainCreateManaged(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	created := frontendDomainCommandResponseWithTLS("app.example.com", "pending_verification", "managed")
	created["verification_status"] = "pending"
	capturedPayload := map[string]any{}
	server := httptest.NewServer(frontendDomainCreateHandler(t, nil, http.StatusCreated, created, capturedPayload))
	defer server.Close()

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"domain", "create", "web", "--domain", "app.example.com", "--tls", "managed")
	require.NoError(t, err)
	assert.Contains(t, out, "Custom domain 'app.example.com' created for frontend 'web'")
	assert.Contains(t, out, "Publish the verification records below and keep them")
	assert.Contains(t, out, "frontends domain get web")
	assert.Contains(t, out, "TLS mode: managed")
	assert.Contains(t, out, "TXT _volcano.app.example.com -> verify")
	assert.Equal(t, map[string]any{"domain": "app.example.com", "tls": map[string]any{"mode": "managed"}}, capturedPayload)
}

func TestFrontendsDomainCreateSwitchesTLSModeInPlace(t *testing.T) {
	t.Run("to managed", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		switched := frontendDomainCommandResponseWithTLS("app.example.com", "active", "managed")
		switched["serving_certificate"] = "uploaded"
		switched["verification_records"] = []any{map[string]any{
			"type": "CNAME", "name": "_acme-challenge.app.example.com", "value": "app.example.com.acme.volcano.run",
		}}
		server := httptest.NewServer(frontendDomainCreateHandler(t,
			frontendDomainCommandResponse("app.example.com", "active"), http.StatusOK, switched, nil))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domain", "create", "web", "--domain", "app.example.com", "--tls", "managed")
		require.NoError(t, err)
		assert.Contains(t, out, "Custom domain 'app.example.com' on frontend 'web' is switching to managed TLS")
		assert.Contains(t, out, "Your uploaded certificate keeps serving until Volcano issues the managed one")
		assert.Contains(t, out, "Serving certificate: uploaded")
		assert.Contains(t, out, "CNAME _acme-challenge.app.example.com -> app.example.com.acme.volcano.run")
	})

	t.Run("to byoc", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		previous := frontendDomainCommandResponseWithTLS("app.example.com", "active", "managed")
		switched := frontendDomainCommandResponse("app.example.com", "active")
		switched["serving_certificate"] = "uploaded"
		server := httptest.NewServer(frontendDomainCreateHandler(t, previous, http.StatusOK, switched, nil))
		defer server.Close()

		certPath, keyPath := writeFrontendDomainCertificate(t)
		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domain", "create", "web", "--domain", "app.example.com", "--cert", certPath, "--key", keyPath)
		require.NoError(t, err)
		assert.Contains(t, out, "Custom domain 'app.example.com' on frontend 'web' switched to BYOC TLS")
		assert.Contains(t, out, "You can delete the _acme-challenge CNAME")
	})
}

func TestFrontendsDomainCreateRepeatedBYOCKeepsTheCertificate(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	existing := frontendDomainCommandResponse("app.example.com", "active")
	server := httptest.NewServer(frontendDomainCreateHandler(t, existing, http.StatusOK, existing, nil))
	defer server.Close()

	certPath, keyPath := writeFrontendDomainCertificate(t)
	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"domain", "create", "web", "--domain", "app.example.com", "--cert", certPath, "--key", keyPath)
	require.NoError(t, err)
	assert.Contains(t, out, "Custom domain 'app.example.com' already uses byoc TLS on frontend 'web'")
	assert.Contains(t, out, "The certificate was not replaced")
	assert.Contains(t, out, "config deploy")
}

func TestFrontendsDomainCreateChecksTLSFlagsBeforeCallingTheAPI(t *testing.T) {
	certPath, keyPath := writeFrontendDomainCertificate(t)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"managed with a certificate", []string{"--tls", "managed", "--cert", certPath}, "--cert, --key, and --chain apply only to --tls byoc"},
		{"managed with a chain", []string{"--tls", "managed", "--chain", certPath}, "--cert, --key, and --chain apply only to --tls byoc"},
		{"byoc without a key", []string{"--cert", certPath}, "--cert and --key are required with --tls byoc"},
		{"no TLS flags", nil, "use --tls managed to have Volcano issue the certificate"},
		{"unknown mode", []string{"--tls", "auto", "--cert", certPath, "--key", keyPath}, `invalid --tls "auto": use managed or byoc`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				http.NotFound(w, r)
			}))
			defer server.Close()

			args := append([]string{"domain", "create", "web", "--domain", "app.example.com"}, tc.args...)
			_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestFrontendsDomainGetWithoutADomain(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(frontendDomainCreateHandler(t, nil, http.StatusCreated, nil, nil))
	defer server.Close()

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domain", "get", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `frontend "web" has no custom domain`)
}

// frontendDomainCreateHandler serves frontend "web" whose custom domain reads
// as previous, or JSON null when previous is nil, and answers a create with
// status and response. A non-nil captured receives the create body. It fails
// the test when the create comes before the read.
func frontendDomainCreateHandler(t *testing.T, previous map[string]any, status int, response, captured map[string]any) http.HandlerFunc {
	t.Helper()
	read := false
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{frontendCommandPayload(frontendID, "web")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			read = true
			writeFrontendCommandJSON(t, w, http.StatusOK, previous)
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/domain":
			assert.True(t, read, "create should read the frontend's domain first")
			if captured != nil {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			}
			writeFrontendCommandJSON(t, w, status, response)
		default:
			http.NotFound(w, r)
		}
	}
}

func writeFrontendDomainCertificate(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600))
	return certPath, keyPath
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
	return frontendDomainCommandResponseWithTLS(domain, status, "byoc")
}

func frontendDomainCommandResponseWithTLS(domain, status, tlsMode string) map[string]any {
	return map[string]any{
		"created_at":              "2026-05-20T00:00:00Z",
		"domain":                  domain,
		"domain_status":           status,
		"effective_urls":          []string{"https://web.frontends.volcano.run/", "https://" + domain + "/"},
		"tls_mode":                tlsMode,
		"updated_at":              "2026-05-20T00:00:00Z",
		"verification_status":     "verified",
		"routing_target_hostname": "web.frontends.volcano.run",
		"verification_records": []any{
			map[string]any{"type": "TXT", "name": "_volcano." + domain, "value": "verify"},
		},
	}
}
