package account

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	currentSessionID  = "11111111-1111-4111-8111-111111111111"
	browserSessionID  = "22222222-2222-4222-8222-222222222222"
	otherCLISessionID = "33333333-3333-4333-8333-333333333333"
	accountToken      = "pk-account"
)

// sessionsAPI is a fake of the account session routes. It records each
// request as "METHOD /path".
type sessionsAPI struct {
	t *testing.T
	// sessions is what GET /user/sessions lists.
	sessions []map[string]any
	// status, when set, is every route's answer instead, with message as its
	// error, or the status text when message is empty.
	status  int
	message string

	mu    sync.Mutex
	calls []string
}

func newSessionsAPI(t *testing.T) *sessionsAPI {
	t.Helper()
	return &sessionsAPI{t: t, sessions: []map[string]any{
		sessionPayload(currentSessionID, "device:volcano-cli", "volcano-cli/0.58.0", true),
		sessionPayload(browserSessionID, "email", "Mozilla/5.0 (Macintosh)", false),
		sessionPayload(otherCLISessionID, "device:volcano-cli", "volcano-cli/0.57.0", false),
	}}
}

func sessionPayload(id, provider, userAgent string, current bool) map[string]any {
	kind := "browser"
	if strings.HasPrefix(provider, "device:") {
		kind = "cli"
	}
	return map[string]any{
		"id":               id,
		"user_id":          "44444444-4444-4444-8444-444444444444",
		"provider":         provider,
		"kind":             kind,
		"user_agent":       userAgent,
		"ip_address":       "203.0.113.7",
		"expires_at":       "2026-12-01T00:00:00Z",
		"last_activity_at": "2026-10-29T12:00:00Z",
		"is_active":        true,
		"is_current":       current,
	}
}

func (a *sessionsAPI) serve() cliruntime.Deps {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.calls = append(a.calls, r.Method+" "+r.URL.Path)
		a.mu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer "+accountToken {
			a.t.Errorf("unexpected Authorization header %q", got)
		}
		if a.status != 0 {
			message := a.message
			if message == "" {
				message = http.StatusText(a.status)
			}
			writeAccountJSON(a.t, w, a.status, map[string]any{"error": message})
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/user/sessions":
			page, limit := queryInt(r, "page", 1), queryInt(r, "limit", 20)
			start, end := min((page-1)*limit, len(a.sessions)), min(page*limit, len(a.sessions))
			writeAccountJSON(a.t, w, http.StatusOK, map[string]any{
				"sessions": a.sessions[start:end], "total": len(a.sessions), "page": page, "limit": limit,
				"total_pages": (len(a.sessions) + limit - 1) / limit,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/user/sessions":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/user/sessions/"):
			for _, session := range a.sessions {
				if session["id"] == strings.TrimPrefix(r.URL.Path, "/user/sessions/") {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			writeAccountJSON(a.t, w, http.StatusNotFound, map[string]any{"error": "session not found"})
		default:
			writeAccountJSON(a.t, w, http.StatusNotFound, map[string]any{"error": "not found"})
		}
	}))
	a.t.Cleanup(server.Close)
	return cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

func queryInt(r *http.Request, name string, fallback int) int {
	if value, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}

func (a *sessionsAPI) requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

func setAccountTestHome(t *testing.T, token string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	t.Setenv("CI", "")
	t.Setenv("VOLCANO_NONINTERACTIVE", "")
	if token != "" {
		require.NoError(t, (&cliconfig.Config{UserToken: token}).Save())
	}
}

func executeAccountCommand(t *testing.T, deps cliruntime.Deps, input string, args ...string) (string, error) {
	t.Helper()
	cmd := New(deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeAccountJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("failed to encode the response: %v", err)
	}
}
