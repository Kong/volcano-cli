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

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const frontendDomainsPath = "/projects/" + frontendProjectID + "/frontends/" + frontendID + "/domains"

// frontendDomainsServer resolves frontend "web" and hands every other request
// to routes, keyed by method and path.
func frontendDomainsServer(t *testing.T, routes map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends" {
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{frontendCommandPayload(frontendID, "web")}, "has_more": false, "page": 1, "limit": 100, "total": 1,
			})
			return
		}
		if handle, ok := routes[r.Method+" "+r.URL.Path]; ok {
			handle(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func frontendRedirectDomainResponse(domain, to string, status int) map[string]any {
	response := frontendDomainCommandResponse(domain, "active")
	response["redirect"] = map[string]any{"to": to, "status": status}
	return response
}

func TestFrontendsDomainsList(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := frontendDomainsServer(t, map[string]http.HandlerFunc{
		"GET " + frontendDomainsPath: func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{
				frontendDomainCommandResponse("www.example.com", "active"),
				frontendRedirectDomainResponse("example.com", "www.example.com", 308),
			}})
		},
	})

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domains", "list", "web")
	require.NoError(t, err)
	lines := strings.Split(out, "\n")
	require.GreaterOrEqual(t, len(lines), 4)
	assert.Contains(t, lines[0], "Redirects to")
	assert.Regexp(t, `^www\.example\.com\s+byoc\s+active\s+.+\s-$`, lines[2])
	assert.Regexp(t, `^example\.com\s+byoc\s+active\s+.+\swww\.example\.com \(308\)$`, lines[3])
	assert.Contains(t, out, "Total: 2 custom domain(s)")
}

func TestFrontendsDomainsListEmpty(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := frontendDomainsServer(t, map[string]http.HandlerFunc{
		"GET " + frontendDomainsPath: func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
		},
	})

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "domains", "list", "web")
	require.NoError(t, err)
	assert.Equal(t, "No custom domains on frontend \"web\"\n", out)
}

func TestFrontendsDomainsGet(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := frontendDomainsServer(t, map[string]http.HandlerFunc{
		"GET " + frontendDomainsPath + "/example.com": func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendRedirectDomainResponse("example.com", "www.example.com", 302))
		},
		"GET " + frontendDomainsPath + "/shop.example.com": func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusNotFound, map[string]any{"error": "custom domain not found"})
		},
	})
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeFrontendsCommand(t, New(deps), "domains", "get", "web", "example.com")
	require.NoError(t, err)
	assert.Contains(t, out, "Frontend: web")
	assert.Contains(t, out, "Domain: example.com")
	assert.Contains(t, out, "Redirects to: www.example.com (302)")

	_, err = executeFrontendsCommand(t, New(deps), "domains", "get", "web", "shop.example.com")
	require.EqualError(t, err, `failed to get custom domain "shop.example.com" on frontend "web": HTTP 404: custom domain not found`)
	assert.Equal(t, http.StatusNotFound, api.Status(err))

	// A server without the domains routes answers a bare 404 of its own.
	_, err = executeFrontendsCommand(t, New(deps), "domains", "get", "web", "app.example.com")
	require.EqualError(t, err, `failed to get custom domain "app.example.com" on frontend "web": HTTP 404: 404 page not found`)
	assert.Equal(t, http.StatusNotFound, api.Status(err))
}

func TestFrontendsDomainsAdd(t *testing.T) {
	t.Run("managed redirect", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var payload map[string]any
		server := frontendDomainsServer(t, map[string]http.HandlerFunc{
			"POST " + frontendDomainsPath: func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				writeFrontendCommandJSON(t, w, http.StatusCreated, frontendRedirectDomainResponse("example.com", "www.example.com", 308))
			},
		})

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domains", "add", "web", "example.com", "--redirect-to", "www.example.com")
		require.NoError(t, err)
		assert.Contains(t, out, "Custom domain 'example.com' added to frontend 'web'")
		assert.Contains(t, out, "Redirects to: www.example.com (308)")
		assert.Equal(t, map[string]any{
			"domain":   "example.com",
			"tls":      map[string]any{"mode": "managed"},
			"redirect": map[string]any{"to": "www.example.com"},
		}, payload)
	})

	t.Run("byoc serving", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var payload map[string]any
		server := frontendDomainsServer(t, map[string]http.HandlerFunc{
			"POST " + frontendDomainsPath: func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				writeFrontendCommandJSON(t, w, http.StatusCreated, frontendDomainCommandResponse("app.example.com", "provisioning"))
			},
		})

		dir := t.TempDir()
		certPath := filepath.Join(dir, "cert.pem")
		keyPath := filepath.Join(dir, "key.pem")
		chainPath := filepath.Join(dir, "chain.pem")
		require.NoError(t, os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"), 0o600))
		require.NoError(t, os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600))
		require.NoError(t, os.WriteFile(chainPath, []byte("-----BEGIN CERTIFICATE-----\nchain\n-----END CERTIFICATE-----\n"), 0o600))

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domains", "add", "web", "app.example.com", "--cert", certPath, "--key", keyPath, "--chain", chainPath)
		require.NoError(t, err)
		assert.Contains(t, out, "Custom domain 'app.example.com' added to frontend 'web'")
		assert.NotContains(t, out, "Redirects to")
		assert.NotContains(t, payload, "redirect")
		tlsConfig, ok := payload["tls"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "byoc", tlsConfig["mode"])
		assert.Contains(t, tlsConfig["certificate_pem"], "cert")
		assert.Contains(t, tlsConfig["private_key_pem"], "key")
		assert.Contains(t, tlsConfig["certificate_chain_pem"], "chain")
	})

	t.Run("already on the frontend", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := frontendDomainsServer(t, map[string]http.HandlerFunc{
			"POST " + frontendDomainsPath: func(w http.ResponseWriter, _ *http.Request) {
				response := frontendDomainCommandResponse("app.example.com", "provisioning")
				response["tls_mode"] = "managed"
				writeFrontendCommandJSON(t, w, http.StatusOK, response)
			},
		})

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domains", "add", "web", "app.example.com")
		require.NoError(t, err)
		assert.Contains(t, out, "Custom domain 'app.example.com' was already on frontend 'web' (TLS: managed)")
		assert.NotContains(t, out, "added to frontend")
	})

	t.Run("over the plan limit", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := frontendDomainsServer(t, map[string]http.HandlerFunc{
			"POST " + frontendDomainsPath: func(w http.ResponseWriter, _ *http.Request) {
				writeFrontendCommandJSON(t, w, http.StatusForbidden, map[string]any{
					"error": "custom domain limit exceeded for your plan (1 per frontend)",
				})
			},
		})

		_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domains", "add", "web", "www.example.com")
		require.ErrorContains(t, err, `failed to add custom domain "www.example.com"`)
		require.ErrorContains(t, err, "HTTP 403: custom domain limit exceeded for your plan (1 per frontend)")
	})
}

func TestFrontendsDomainsUpdate(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		// current answers the read of the domain, which only a redirect
		// without --redirect-status makes.
		current  map[string]any
		response map[string]any
		redirect any
		message  string
	}{
		{
			name:     "redirect",
			args:     []string{"--redirect-to", "www.example.com", "--redirect-status", "302"},
			response: frontendRedirectDomainResponse("example.com", "www.example.com", 302),
			redirect: map[string]any{"to": "www.example.com", "status": float64(302)},
			message:  "Custom domain 'example.com' now redirects to 'www.example.com' with 302",
		},
		{
			name:     "retarget keeps the status",
			args:     []string{"--redirect-to", "app.example.com"},
			current:  frontendRedirectDomainResponse("example.com", "www.example.com", 302),
			response: frontendRedirectDomainResponse("example.com", "app.example.com", 302),
			redirect: map[string]any{"to": "app.example.com", "status": float64(302)},
			message:  "Custom domain 'example.com' now redirects to 'app.example.com' with 302",
		},
		{
			name:     "serving domain takes the default status",
			args:     []string{"--redirect-to", "www.example.com"},
			current:  frontendDomainCommandResponse("example.com", "active"),
			response: frontendRedirectDomainResponse("example.com", "www.example.com", 308),
			redirect: map[string]any{"to": "www.example.com"},
			message:  "Custom domain 'example.com' now redirects to 'www.example.com' with 308",
		},
		{
			name:     "serve again",
			args:     []string{"--no-redirect"},
			response: frontendDomainCommandResponse("example.com", "active"),
			redirect: nil,
			message:  "Custom domain 'example.com' now serves frontend 'web'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFrontendCommandTestHome(t)
			saveFrontendCommandTestConfig(t)
			var payload map[string]any
			routes := map[string]http.HandlerFunc{
				"PATCH " + frontendDomainsPath + "/example.com": func(w http.ResponseWriter, r *http.Request) {
					assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
					writeFrontendCommandJSON(t, w, http.StatusOK, tc.response)
				},
			}
			if tc.current != nil {
				routes["GET "+frontendDomainsPath+"/example.com"] = func(w http.ResponseWriter, _ *http.Request) {
					writeFrontendCommandJSON(t, w, http.StatusOK, tc.current)
				}
			}
			server := frontendDomainsServer(t, routes)

			out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
				append([]string{"domains", "update", "web", "example.com"}, tc.args...)...)
			require.NoError(t, err)
			assert.Contains(t, out, tc.message)
			require.Contains(t, payload, "redirect")
			assert.Equal(t, tc.redirect, payload["redirect"])
		})
	}
}

func TestFrontendsDomainsUpdateNamesTheRedirectsInTheWay(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := frontendDomainsServer(t, map[string]http.HandlerFunc{
		"GET " + frontendDomainsPath + "/www.example.com": func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendDomainCommandResponse("www.example.com", "active"))
		},
		"PATCH " + frontendDomainsPath + "/www.example.com": func(w http.ResponseWriter, _ *http.Request) {
			writeFrontendCommandJSON(t, w, http.StatusConflict, map[string]any{
				"error": "www.example.com is the redirect target of example.com; remove or retarget those redirects first",
			})
		},
	})

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"domains", "update", "web", "www.example.com", "--redirect-to", "app.example.com")
	require.ErrorContains(t, err, `failed to update custom domain "www.example.com"`)
	require.ErrorContains(t, err, "HTTP 409: www.example.com is the redirect target of example.com")
}

func TestFrontendsDomainsRemove(t *testing.T) {
	removeServer := func(t *testing.T, sawDelete *bool) *httptest.Server {
		t.Helper()
		return frontendDomainsServer(t, map[string]http.HandlerFunc{
			"GET " + frontendDomainsPath + "/example.com": func(w http.ResponseWriter, _ *http.Request) {
				writeFrontendCommandJSON(t, w, http.StatusOK, frontendRedirectDomainResponse("example.com", "www.example.com", 308))
			},
			"DELETE " + frontendDomainsPath + "/example.com": func(w http.ResponseWriter, _ *http.Request) {
				*sawDelete = true
				w.WriteHeader(http.StatusNoContent)
			},
		})
	}

	t.Run("cancel", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
		server := removeServer(t, &sawDelete)

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("no\n"))
		out, err := executeFrontendsCommand(t, cmd, "domains", "remove", "web", "example.com")
		require.NoError(t, err)
		assert.False(t, sawDelete)
		assert.Contains(t, out, "Delete custom domain 'example.com on frontend web'?")
		assert.Contains(t, out, "Delete cancelled.")
	})

	t.Run("yes", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var sawDelete bool
		server := removeServer(t, &sawDelete)

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			"domains", "remove", "web", "example.com", "--yes")
		require.NoError(t, err)
		assert.True(t, sawDelete)
		assert.Contains(t, out, "Custom domain 'example.com' removal started")
	})
}

func TestFrontendsDomainsNameAMissingFrontend(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends" {
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false, "page": 1, "limit": 100, "total": 0})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"list", "web"},
		{"get", "web", "example.com"},
		{"add", "web", "example.com", "--redirect-to", "www.example.com"},
		{"update", "web", "example.com", "--no-redirect"},
		{"remove", "web", "example.com", "--yes"},
	} {
		_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			append([]string{"domains"}, args...)...)
		require.EqualError(t, err, `frontend "web" not found`, "%v", args)
	}
}

// Flag mistakes are refused before the CLI resolves the frontend.
func TestFrontendsDomainsRefuseBadFlags(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("-----BEGIN CERTIFICATE-----\ncert\n-----END CERTIFICATE-----\n"), 0o600))
	require.NoError(t, os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600))

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"update", "web", "example.com"}, "specify --redirect-to or --no-redirect"},
		{[]string{"update", "web", "example.com", "--redirect-to", "www.example.com", "--no-redirect"}, "none of the others can be"},
		{[]string{"update", "web", "example.com", "--no-redirect", "--redirect-status", "302"}, "--redirect-status needs --redirect-to"},
		{[]string{"add", "web", "example.com", "--redirect-to", "www.example.com", "--redirect-status", "303"}, "invalid --redirect-status 303: use 301, 302, 307, or 308"},
		{[]string{"add", "web", "example.com", "--redirect-status", "301"}, "--redirect-status needs --redirect-to"},
		{[]string{"add", "web", "example.com", "--redirect-to", ""}, "--redirect-to needs a domain"},
		{[]string{"update", "web", "example.com", "--redirect-to", " "}, "--redirect-to needs a domain"},
		{[]string{"add", "web", "example.com", "--cert", "cert.pem"}, "they must all be set; missing [key]"},
		{[]string{"add", "web", "example.com", "--chain", "chain.pem"}, "--chain needs --cert and --key"},
		{[]string{"add", "web", "example.com", "--chain", ""}, "--chain needs --cert and --key"},
		{[]string{"add", "web", "example.com", "--cert", "", "--key", ""}, "certificate file path is required"},
		{[]string{"add", "web", "example.com", "--cert", certPath, "--key", ""}, "private key file path is required"},
		{[]string{"add", "web", "example.com", "--cert", certPath, "--key", keyPath, "--chain", ""}, "certificate chain file path is required"},
		{[]string{"list"}, "accepts 1 arg(s), received 0"},
		{[]string{"add", "web"}, "accepts 2 arg(s), received 1"},
		{[]string{"get", "web"}, "accepts 2 arg(s), received 1"},
		{[]string{"update", "web"}, "accepts 2 arg(s), received 1"},
		{[]string{"remove", "web"}, "accepts 2 arg(s), received 1"},
	} {
		_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
			append([]string{"domains"}, tc.args...)...)
		require.ErrorContains(t, err, tc.want, "%v", tc.args)
	}
}
