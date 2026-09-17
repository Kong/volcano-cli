package accesstokens

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestAccessTokenCommandsCreateListGetRevoke(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var createBodies []map[string]any
	var listQueries []string
	var sawRevoke bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		basePath := "/projects/" + accessTokenProjectID + "/access-tokens"
		switch {
		case r.Method == http.MethodPost && r.URL.Path == basePath:
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			createBodies = append(createBodies, body)
			created := accessTokenCommandPayload(accessTokenID, "ci-deploy")
			created["token"] = "pt-Wq9l2m4XcR7tFv1sN8bK3hJ0"
			writeAccessTokenCommandJSON(t, w, http.StatusCreated, created)
		case r.Method == http.MethodGet && r.URL.Path == basePath:
			listQueries = append(listQueries, r.URL.RawQuery)
			writeAccessTokenCommandJSON(t, w, http.StatusOK,
				accessTokenCommandPage(accessTokenCommandPayload(accessTokenID, "ci-deploy")))
		case r.Method == http.MethodGet && r.URL.Path == basePath+"/"+accessTokenID:
			writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
		case r.Method == http.MethodDelete && r.URL.Path == basePath+"/"+accessTokenID:
			sawRevoke = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "create", "ci-deploy", "--scope", "read_only", "--expires-at", "2099-01-31T00:00:00Z")
	require.NoError(t, err)
	require.Len(t, createBodies, 1)
	assert.Equal(t, map[string]any{
		"name":       "ci-deploy",
		"scope":      "read_only",
		"expires_at": "2099-01-31T00:00:00Z",
	}, createBodies[0])
	assert.Contains(t, out, "Access token 'ci-deploy' created")
	assert.Contains(t, out, "pt-Wq9l2m4XcR7tFv1sN8bK3hJ0")
	assert.Contains(t, out, "shown once and cannot be retrieved again")

	out, err = executeAccessTokenCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=100"}, listQueries)
	assert.Contains(t, out, "ci-deploy")
	assert.Contains(t, out, "Name                      Prefix            Scope       Status     Last used        Requests")
	assert.Contains(t, out, "ci-deploy                 pt-Wq9l2m4X       full        active     3h ago           42")
	assert.Contains(t, out, "Showing 1 of 1 access token(s) (page 1, limit 100)")

	out, err = executeAccessTokenCommand(t, New(deps), "get", accessTokenID)
	require.NoError(t, err)
	assert.Contains(t, out, "Name: ci-deploy")
	assert.Contains(t, out, "Requests: 42")
	assert.Contains(t, out, "Expires: never")

	out, err = executeAccessTokenCommand(t, New(deps), "revoke", "ci-deploy", "--yes")
	require.NoError(t, err)
	assert.True(t, sawRevoke)
	assert.Contains(t, out, "Access token 'ci-deploy' revoked")
}

func TestAccessTokenListFlagsAndJSON(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeAccessTokenCommandJSON(t, w, http.StatusOK,
			accessTokenCommandPage(accessTokenCommandPayload(accessTokenID, "ci-deploy")))
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "list", "--page", "2", "--limit", "25", "--search", "ci", "--include-revoked", "--json")
	require.NoError(t, err)
	assert.Equal(t, "page=2&limit=25&search=ci&include_revoked=true", query)

	var page struct {
		Data []struct {
			Name        string `json:"name"`
			TokenPrefix string `json:"token_prefix"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &page))
	require.Len(t, page.Data, 1)
	assert.Equal(t, "ci-deploy", page.Data[0].Name)
	assert.Equal(t, "pt-Wq9l2m4X", page.Data[0].TokenPrefix)
}

func TestAccessTokenGetWithUsage(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var usageQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		basePath := "/projects/" + accessTokenProjectID + "/access-tokens"
		switch r.URL.Path {
		case basePath + "/" + accessTokenID:
			writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
		case basePath + "/" + accessTokenID + "/usage":
			usageQuery = r.URL.RawQuery
			writeAccessTokenCommandJSON(t, w, http.StatusOK,
				accessTokenCommandUsagePayload(accessTokenID, "ci-deploy", 3, 0))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "get", accessTokenID, "--usage", "--days", "7")
	require.NoError(t, err)
	assert.Equal(t, "days=7", usageQuery)
	assert.Contains(t, out, "Name: ci-deploy")
	assert.Contains(t, out, "2026-09-14")
	assert.Contains(t, out, "3 request(s) over 2 day(s)")

	out, err = executeAccessTokenCommand(t, New(deps), "get", accessTokenID, "--usage", "--json")
	require.NoError(t, err)
	assert.Equal(t, "days=30", usageQuery)
	var payload struct {
		Token struct {
			Name string `json:"name"`
		} `json:"token"`
		Usage struct {
			Days  int `json:"days"`
			Daily []struct {
				Day      string `json:"day"`
				Requests int    `json:"requests"`
			} `json:"daily"`
			TotalRequests int `json:"total_requests"`
		} `json:"usage"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	assert.Equal(t, "ci-deploy", payload.Token.Name)
	assert.Equal(t, 2, payload.Usage.Days)
	assert.Equal(t, 3, payload.Usage.TotalRequests)
	require.Len(t, payload.Usage.Daily, 2)
	assert.Equal(t, "2026-09-14", payload.Usage.Daily[0].Day)
	assert.Equal(t, 3, payload.Usage.Daily[0].Requests)

	_, err = executeAccessTokenCommand(t, New(deps), "get", accessTokenID, "--days", "7")
	require.ErrorContains(t, err, "--days applies to --usage")
}

// --usage used to resolve the identifier a second time to fetch the counts.
// For a name that is another paginated walk, and a transient failure on it
// reports a token as missing moments after the same lookup found it.
func TestAccessTokenGetWithUsageResolvesTheNameOnce(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var listRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		basePath := "/projects/" + accessTokenProjectID + "/access-tokens"
		switch r.URL.Path {
		case basePath:
			listRequests++
			writeAccessTokenCommandJSON(t, w, http.StatusOK,
				accessTokenCommandPage(accessTokenCommandPayload(accessTokenID, "ci-deploy")))
		case basePath + "/" + accessTokenID + "/usage":
			writeAccessTokenCommandJSON(t, w, http.StatusOK,
				accessTokenCommandUsagePayload(accessTokenID, "ci-deploy", 3, 0))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeAccessTokenCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"get", "ci-deploy", "--usage", "--days", "7")
	require.NoError(t, err)
	assert.Equal(t, 1, listRequests, "the name must be resolved once, not once per request")
	assert.Contains(t, out, "Name: ci-deploy")
	assert.Contains(t, out, "3 request(s) over 2 day(s)")
}

// The filter is a liveness test: it hides every token that can no longer
// authenticate, expired ones as well as revoked. Help that names only revoked
// ones describes a narrower flag than the one the user gets — and leaves the
// "expired" status the list can print unexplained.
func TestAccessTokenListHelpNamesExpiredTokensToo(t *testing.T) {
	out, err := executeAccessTokenCommand(t, New(cliruntime.Deps{}), "list", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "Include revoked and expired tokens")
	assert.Contains(t, out, "revoked and expired alike")
}

// The usage reads are what a pt- token can run. 'get --usage' is not one of
// them: it reads the token's record first, which the CLI refuses and the API
// denies — so help that says "usage is the exception" without qualifying it
// promises a command that does not work.
func TestAccessTokenHelpLimitsTheUsageExceptionToTheUsageReads(t *testing.T) {
	out, err := executeAccessTokenCommand(t, New(cliruntime.Deps{}), "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "The 'usage' reads are the exception")
	assert.Contains(t, out, "'get --usage' is not")
}

func TestAccessTokenProjectUsage(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/"+accessTokenProjectID+"/access-tokens/usage" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.RawQuery
		writeAccessTokenCommandJSON(t, w, http.StatusOK, []any{
			accessTokenCommandUsagePayload(accessTokenID, "ci-deploy", 3, 4),
			// Revoked tokens keep their history, so the project view still carries them.
			accessTokenCommandUsagePayload("88888888-8888-4888-8888-888888888888", "ci-retired", 1, 0),
		})
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "usage", "--days", "2")
	require.NoError(t, err)
	assert.Equal(t, "days=2", query)
	assert.Contains(t, out, "ci-deploy                 7")
	assert.Contains(t, out, "ci-retired                1")
	assert.Contains(t, out, "8 request(s) across 2 token(s) over 2 day(s)")

	out, err = executeAccessTokenCommand(t, New(deps), "usage", "--json")
	require.NoError(t, err)
	assert.Equal(t, "days=30", query)
	var payload []struct {
		Name          string `json:"name"`
		Days          int    `json:"days"`
		TotalRequests int    `json:"total_requests"`
		Daily         []struct {
			Requests int `json:"requests"`
		} `json:"daily"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	require.Len(t, payload, 2)
	assert.Equal(t, "ci-deploy", payload[0].Name)
	assert.Equal(t, 7, payload[0].TotalRequests)
	assert.Equal(t, 2, payload[0].Days)
	require.Len(t, payload[0].Daily, 2)
	assert.Equal(t, 4, payload[0].Daily[1].Requests)
}

func TestAccessTokenProjectUsageWithoutTokens(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAccessTokenCommandJSON(t, w, http.StatusOK, []any{})
	}))
	defer server.Close()

	out, err := executeAccessTokenCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "usage")
	require.NoError(t, err)
	assert.Contains(t, out, "No access tokens created")
}

func TestAccessTokenResolvesAndRevokesByName(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")

	var listQuery string
	var revokedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		basePath := "/projects/" + accessTokenProjectID + "/access-tokens"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == basePath:
			listQuery = r.URL.RawQuery
			writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPage(
				// A substring match the exact name must win over.
				accessTokenCommandPayload("88888888-8888-4888-8888-888888888888", "ci-deploy-staging"),
				accessTokenCommandPayload(accessTokenID, "ci-deploy"),
			))
		case r.Method == http.MethodDelete:
			revokedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "revoke", "ci-deploy", "--yes")
	require.NoError(t, err)
	// Revoking by name has to find a token it already revoked, so the lookup
	// includes revoked records.
	assert.Equal(t, "page=1&limit=100&search=ci-deploy&include_revoked=true", listQuery)
	assert.Equal(t, "/projects/"+accessTokenProjectID+"/access-tokens/"+accessTokenID, revokedPath)
	assert.Contains(t, out, "Access token 'ci-deploy' revoked")
}

func TestAccessTokenGetUnknownName(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPage())
	}))
	defer server.Close()

	_, err := executeAccessTokenCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", "missing")
	require.ErrorContains(t, err, `access token "missing" not found`)
}

func TestAccessTokenRevokePromptAndYes(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		setAccessTokenCommandTestHome(t)
		saveAccessTokenCommandTestConfig(t, "token")
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				assert.Fail(t, "unexpected revoke after a cancelled prompt", r.URL.Path)
				return
			}
			writeAccessTokenCommandJSON(t, w, http.StatusOK,
				accessTokenCommandPage(accessTokenCommandPayload(accessTokenID, "ci-deploy")))
		}))
		defer server.Close()

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("no\n"))
		out, err := executeAccessTokenCommand(t, cmd, "revoke", "ci-deploy")
		require.NoError(t, err)
		assert.Contains(t, out, "Revoking a token immediately breaks")
		assert.Contains(t, out, "Revoke access token 'ci-deploy' (pt-Wq9l2m4X)?")
		assert.Contains(t, out, "Cancelled.")
	})

	t.Run("confirm", func(t *testing.T) {
		setAccessTokenCommandTestHome(t)
		saveAccessTokenCommandTestConfig(t, "token")
		var sawRevoke bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				sawRevoke = true
				w.WriteHeader(http.StatusNoContent)
				return
			}
			writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
		}))
		defer server.Close()

		cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
		cmd.SetIn(strings.NewReader("y\n"))
		out, err := executeAccessTokenCommand(t, cmd, "revoke", accessTokenID)
		require.NoError(t, err)
		assert.True(t, sawRevoke)
		// A UUID says nothing about which credential it is, so the prompt names
		// the token it resolved to rather than echoing the argument back.
		assert.Contains(t, out, "Revoke access token 'ci-deploy' (pt-Wq9l2m4X)?")
		assert.NotContains(t, out, "Revoke access token '"+accessTokenID+"'")
		assert.Contains(t, out, "Access token 'ci-deploy' revoked")
	})
}

// A prompt read from a closed stdin comes back as a decline, so the command
// would exit 0 with the token still live. The caller likeliest to hit that is
// an incident script revoking a leaked credential, which reads a green exit as
// the credential being dead.
func TestAccessTokenRevokeRefusesWhenStdinCannotAnswer(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			assert.Fail(t, "unexpected revoke without confirmation", r.URL.Path)
			return
		}
		writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
	}))
	defer server.Close()

	closed, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closed.Close() })

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(closed)
	out, err := executeAccessTokenCommand(t, cmd, "revoke", accessTokenID)

	require.ErrorContains(t, err, "confirmation required; pass --yes")
	assert.NotContains(t, out, "Cancelled.", "a prompt nobody can answer is not a cancellation")
}

// A human who answers "no" still cancels quietly, exit 0, as everywhere else
// in the CLI.
func TestAccessTokenRevokeCancelsQuietlyWhenAnswered(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			assert.Fail(t, "unexpected revoke after a cancelled prompt", r.URL.Path)
			return
		}
		writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader("n\n"))
	out, err := executeAccessTokenCommand(t, cmd, "revoke", accessTokenID)

	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled.")
}

// Asking about a token that does not exist is a question the user cannot
// answer: a typo'd name has to fail as missing, not after they have confirmed
// revoking it.
func TestAccessTokenRevokeResolvesBeforeAsking(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			assert.Fail(t, "unexpected revoke of a token that was never found", r.URL.Path)
			return
		}
		writeAccessTokenCommandJSON(t, w, http.StatusOK, accessTokenCommandPage())
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader("y\n"))
	out, err := executeAccessTokenCommand(t, cmd, "revoke", "ci-deplyo")

	require.ErrorContains(t, err, `access token "ci-deplyo" not found`)
	assert.NotContains(t, out, "Revoke access token", "the prompt must not run before the token is resolved")
}

func TestAccessTokenCreateRejectsBadInput(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request for invalid input", r.URL.Path)
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	for name, test := range map[string]struct {
		args    []string
		message string
	}{
		"no name":      {args: []string{"create"}, message: "specify a token name as an argument or --name"},
		"two names":    {args: []string{"create", "ci", "--name", "ci"}, message: "not both"},
		"bad scope":    {args: []string{"create", "ci", "--scope", "admin"}, message: `invalid scope "admin": expected full or read_only`},
		"bad expiry":   {args: []string{"create", "ci", "--expires-at", "next tuesday"}, message: "expected an RFC3339 timestamp"},
		"past expiry":  {args: []string{"create", "ci", "--expires-at", "2020-01-01T00:00:00Z"}, message: "is in the past"},
		"unknown name": {args: []string{"get"}, message: "accepts 1 arg(s), received 0"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := executeAccessTokenCommand(t, New(deps), test.args...)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestAccessTokenCreateAcceptsTheNameFlag(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeAccessTokenCommandJSON(t, w, http.StatusCreated, accessTokenCommandPayload(accessTokenID, "ci-deploy"))
	}))
	defer server.Close()

	_, err := executeAccessTokenCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "--name", "ci-deploy")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "ci-deploy", "scope": "full"}, body)
}

// A secret shown once is the one thing automation must not have to scrape out
// of human-readable output, which is exactly what this repo's own E2E was doing
// before --json existed.
func TestAccessTokenCreateEmitsJSONIncludingTheSecret(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	const secret = "pt-9f3c1a8b2d47e0c5a1b8f36d92e4c7a0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload := accessTokenCommandPayload(accessTokenID, "ci-deploy")
		payload["token"] = secret
		writeAccessTokenCommandJSON(t, w, http.StatusCreated, payload)
	}))
	defer server.Close()

	out, err := executeAccessTokenCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "ci-deploy", "--json")
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded), "output should parse as JSON: %s", out)
	assert.Equal(t, secret, decoded["token"])
	assert.Equal(t, "ci-deploy", decoded["name"])
}

// A window the API rejects should fail here, not there. The lower bound is the
// one that mattered: a non-positive value was dropped from the request, so the
// caller got the 30-day default and a success exit code — a plausible answer to
// a question they did not ask.
func TestAccessTokenUsageRejectsAWindowTheAPIWouldNot(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, "token")
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request for an invalid window", r.URL.String())
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"usage", "--days", "0"},
		{"usage", "--days", "-5"},
		{"usage", "--days", "61"},
		{"get", "ci-deploy", "--usage", "--days", "0"},
		{"get", "ci-deploy", "--usage", "--days", "900"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := executeAccessTokenCommand(t,
				New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--days")
			assert.Contains(t, err.Error(), "1 to 60")
		})
	}
}

// Minting, revoking, and reading a credential's record are account operations.
// A pt- token would only earn a 403, so the CLI has to say what is missing
// before the request.
func TestAccessTokenCommandsRejectAProjectToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request with a project access token", r.URL.Path)
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	for _, args := range [][]string{
		{"create", "ci-deploy"},
		{"list"},
		{"get", accessTokenID},
		{"revoke", accessTokenID, "--yes"},
	} {
		setAccessTokenCommandTestHome(t)
		saveAccessTokenCommandTestConfig(t, cliconfig.ProjectTokenPrefix+"token")

		_, err := executeAccessTokenCommand(t, New(deps), args...)
		require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "%v", args)
		require.ErrorContains(t, err, "failed to manage access tokens", "%v", args)
	}
}

// Usage is not one of those: the API answers it for a project access token, so
// a CI job holding nothing but the credential it runs with can report its own
// consumption. Refusing it here denied a request the server would have served.
func TestAccessTokenUsageAcceptsAProjectToken(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, cliconfig.ProjectTokenPrefix+"token")

	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/"+accessTokenProjectID+"/access-tokens/usage" {
			http.NotFound(w, r)
			return
		}
		authorization = r.Header.Get("Authorization")
		writeAccessTokenCommandJSON(t, w, http.StatusOK, []any{
			accessTokenCommandUsagePayload(accessTokenID, "ci-deploy", 18, 24),
		})
	}))
	defer server.Close()

	out, err := executeAccessTokenCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "usage", "--days", "7")
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+cliconfig.ProjectTokenPrefix+"token", authorization)
	assert.Contains(t, out, "ci-deploy                 42")
}

// The API serves a token its own day-by-day series, but the only route to it
// was 'get --usage', which resolves the token's record first and so needs an
// account token. A CI job holding just its pt- credential could not read the
// series the platform was willing to give it.
func TestAccessTokenUsageByIDAcceptsAProjectToken(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, cliconfig.ProjectTokenPrefix+"token")

	var paths []string
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/projects/"+accessTokenProjectID+"/access-tokens/"+accessTokenID+"/usage" {
			http.NotFound(w, r)
			return
		}
		query = r.URL.RawQuery
		writeAccessTokenCommandJSON(t, w, http.StatusOK,
			accessTokenCommandUsagePayload(accessTokenID, "ci-deploy", 18, 0, 24))
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeAccessTokenCommand(t, New(deps), "usage", accessTokenID, "--days", "3")
	require.NoError(t, err)
	assert.Equal(t, "days=3", query)
	// No metadata read: the record behind the ID is what a pt- token cannot see.
	assert.Equal(t, []string{"/projects/" + accessTokenProjectID + "/access-tokens/" + accessTokenID + "/usage"}, paths)
	assert.Contains(t, out, "2026-09-15    0")
	assert.Contains(t, out, "42 request(s) over 3 day(s)")

	out, err = executeAccessTokenCommand(t, New(deps), "usage", accessTokenID, "--json")
	require.NoError(t, err)
	var payload struct {
		TokenID       string `json:"token_id"`
		TotalRequests int    `json:"total_requests"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	assert.Equal(t, accessTokenID, payload.TokenID)
	assert.Equal(t, 42, payload.TotalRequests)
}

// A name cannot be turned into an ID without the account-only list endpoint, so
// the argument says what it needs instead of failing as a 403 from a lookup the
// credential was never going to be allowed to make.
func TestAccessTokenUsageRejectsANameArgument(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	saveAccessTokenCommandTestConfig(t, cliconfig.ProjectTokenPrefix+"token")
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request for a name that cannot be resolved", r.URL.Path)
	}))
	defer server.Close()

	_, err := executeAccessTokenCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "usage", "ci-deploy")

	require.ErrorContains(t, err, `invalid token ID "ci-deploy"`)
	assert.ErrorContains(t, err, "volcano access-tokens get ci-deploy --usage")
}

func TestAccessTokenCommandsRequireProject(t *testing.T) {
	setAccessTokenCommandTestHome(t)
	require.NoError(t, (&cliconfig.Config{UserToken: "token"}).Save())

	_, err := executeAccessTokenCommand(t, New(cliruntime.Deps{}), "list")
	require.ErrorContains(t, err, "no project selected. Run 'volcano use <project-name>' or set VOLCANO_PROJECT_ID")
}

// Local development issues no credentials, so the local tree answers the
// command with where it lives instead of letting cobra print the root help and
// exit 0.
func TestLocalTreeSendsAccessTokensToCloud(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"create", "ci-deploy"},
		{"usage"},
		{"revoke", "ci-deploy", "--yes"},
	} {
		_, err := executeAccessTokenCommand(t, NewCloudOnly(), args...)
		require.ErrorContains(t, err, "is a cloud command", "%v", args)
		require.ErrorContains(t, err, "volcano cloud access-tokens", "%v", args)
	}

	stub := NewCloudOnly()
	assert.True(t, stub.Hidden, "local help must not advertise a command local mode cannot run")
	assert.Equal(t, New(cliruntime.Deps{}).Aliases, stub.Aliases, "the stub must answer to every spelling of the real command")
}
