package notifications

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sensitive"
)

const (
	notificationsProjectID = "22222222-2222-4222-8222-222222222222"
	notificationsWebhook   = "https://example.com/hooks/volcano"
	// notificationsSecret stands in for the secret the API returns once.
	notificationsSecret        = "whsec_fake-signing-secret"
	notificationsRotatedSecret = "whsec_fake-rotated-secret"
	// notificationsOwner is the project owner's account address, the only
	// recipient the API accepts.
	notificationsOwner  = "owner@example.com"
	ownerOnlyRecipients = "email.recipients: only the project owner's account email address is supported for now"
)

// notificationsServer is a fake API that keeps the project's settings, so a
// command sequence reads back what the previous command saved.
type notificationsServer struct {
	t        *testing.T
	mu       sync.Mutex
	settings map[string]any
	updates  []map[string]any
	rotated  int
	test     map[string]any
	status   int
}

func newNotificationsServer(t *testing.T) (*notificationsServer, cliruntime.Deps) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	require.NoError(t, (&cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: notificationsProjectID, Name: "Beta"},
	}).Save())

	fake := &notificationsServer{t: t, settings: defaultNotificationSettings()}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

func defaultNotificationSettings() map[string]any {
	return map[string]any{
		"project_id": notificationsProjectID,
		"email":      map[string]any{"enabled": true, "recipients": []any{}},
		"webhook":    map[string]any{"secret_configured": false},
		"scheduler_failures": map[string]any{
			"notify_on": "consecutive_failures", "threshold": 3, "notify_on_recovery": true,
		},
	}
}

func (s *notificationsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer token" {
		s.t.Errorf("request without the account token: %q", r.Header.Get("Authorization"))
	}
	base := "/projects/" + notificationsProjectID + "/notifications"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == base:
		s.write(w, http.StatusOK, s.settings)
	case r.Method == http.MethodPut && r.URL.Path == base:
		var update map[string]any
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			s.t.Errorf("decode update: %v", err)
		}
		s.updates = append(s.updates, update)
		if !ownerOnly(update) {
			s.write(w, http.StatusBadRequest, map[string]any{"error": ownerOnlyRecipients})
			return
		}
		s.write(w, http.StatusOK, s.apply(update))
	case r.Method == http.MethodPost && r.URL.Path == base+"/webhook-secret":
		webhook, _ := s.settings["webhook"].(map[string]any)
		if webhook["url"] == nil {
			s.write(w, http.StatusConflict, map[string]any{"error": "no webhook is configured"})
			return
		}
		s.rotated++
		s.write(w, http.StatusOK, s.withSecret(notificationsRotatedSecret))
	case r.Method == http.MethodPost && r.URL.Path == base+"/test":
		s.write(w, s.status, s.test)
	default:
		http.NotFound(w, r)
	}
}

// ownerOnly reports whether update names no recipient but the owner, as the
// API requires.
func ownerOnly(update map[string]any) bool {
	email, _ := update["email"].(map[string]any)
	recipients, _ := email["recipients"].([]any)
	for _, recipient := range recipients {
		if address, _ := recipient.(string); !strings.EqualFold(address, notificationsOwner) {
			return false
		}
	}
	return true
}

// apply saves update the way the API does, returning the response, which
// carries the secret only when the update generated one. Recipients are saved
// empty, since the only one accepted is the owner.
func (s *notificationsServer) apply(update map[string]any) map[string]any {
	email, _ := s.settings["email"].(map[string]any)
	if changes, ok := update["email"].(map[string]any); ok {
		maps.Copy(email, changes)
		if _, ok := changes["recipients"]; ok {
			email["recipients"] = []any{}
		}
	}
	policy, _ := s.settings["scheduler_failures"].(map[string]any)
	if changes, ok := update["scheduler_failures"].(map[string]any); ok {
		maps.Copy(policy, changes)
	}
	changes, ok := update["webhook"].(map[string]any)
	if !ok {
		return s.settings
	}
	webhook, _ := s.settings["webhook"].(map[string]any)
	switch url, _ := changes["url"].(string); {
	case url == "":
		s.settings["webhook"] = map[string]any{"secret_configured": false}
	case webhook["url"] == nil:
		s.settings["webhook"] = map[string]any{"url": url, "secret_configured": true}
		return s.withSecret(notificationsSecret)
	default:
		webhook["url"] = url
	}
	return s.settings
}

func (s *notificationsServer) withSecret(secret string) map[string]any {
	response := maps.Clone(s.settings)
	webhook, _ := s.settings["webhook"].(map[string]any)
	response["webhook"] = map[string]any{"url": webhook["url"], "secret_configured": true, "secret": secret}
	return response
}

// write answers from the server's goroutine, where require is not valid.
func (s *notificationsServer) write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.t.Errorf("encode response: %v", err)
	}
}

func (s *notificationsServer) lastUpdate(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.NotEmpty(t, s.updates, "no update reached the API")
	return s.updates[len(s.updates)-1]
}

func (s *notificationsServer) updateCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.updates)
}

func executeNotificationsCommand(t *testing.T, cmd *cobra.Command, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestNotificationsGetShowsTheDefaults(t *testing.T) {
	_, deps := newNotificationsServer(t)

	out, err := executeNotificationsCommand(t, New(deps), "", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "Email: on, to the project owner")
	assert.Contains(t, out, "Webhook: not configured")
	assert.Contains(t, out, "Alert on: 3 failed scheduled runs in a row")
	assert.Contains(t, out, "Recovery: notify when a failing scheduler succeeds again")
}

// The secret is the one thing the user cannot get back, so the command that
// receives it has to print it, and nothing else may.
func TestNotificationsWebhookSecretIsShownOnce(t *testing.T) {
	fake, deps := newNotificationsServer(t)

	out, err := executeNotificationsCommand(t, New(deps), "", "set", "--webhook-url", notificationsWebhook)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"webhook": map[string]any{"url": notificationsWebhook}}, fake.lastUpdate(t),
		"only the flag passed is sent, so every other setting keeps its value")
	assert.Contains(t, out, "Notification settings saved")
	assert.Contains(t, out, "Webhook: "+notificationsWebhook)
	assert.Contains(t, out, "Signing secret: "+notificationsSecret)
	assert.Contains(t, out, "shown once and cannot be retrieved again")

	out, err = executeNotificationsCommand(t, New(deps), "", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "Signing secret: configured")
	assert.NotContains(t, out, notificationsSecret)

	out, err = executeNotificationsCommand(t, New(deps), "", "set", "--webhook-url", notificationsWebhook+"/v2")
	require.NoError(t, err)
	assert.NotContains(t, out, "whsec_", "changing the URL keeps the secret, so none is printed")
	assert.NotContains(t, out, "shown once")

	out, err = executeNotificationsCommand(t, New(deps), "", "rotate-webhook-secret", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Webhook signing secret rotated")
	assert.Contains(t, out, "Signing secret: "+notificationsRotatedSecret)

	out, err = executeNotificationsCommand(t, New(deps), "", "rotate-webhook-secret", "--yes", "--json")
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &settings), "--json emits only JSON: %s", out)
	webhook, _ := settings["webhook"].(map[string]any)
	assert.Equal(t, notificationsRotatedSecret, webhook["secret"], "--json carries the new secret for automation")
}

func TestNotificationsSetSendsEveryFlagPassed(t *testing.T) {
	fake, deps := newNotificationsServer(t)

	_, err := executeNotificationsCommand(t, New(deps), "", "set",
		"--email=false", "--recipients", " "+notificationsOwner,
		"--notify-on", "first_failure", "--threshold", "5", "--notify-on-recovery=false")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"email": map[string]any{
			"enabled":    false,
			"recipients": []any{notificationsOwner},
		},
		"scheduler_failures": map[string]any{
			"notify_on": "first_failure", "threshold": float64(5), "notify_on_recovery": false,
		},
	}, fake.lastUpdate(t))

	_, err = executeNotificationsCommand(t, New(deps), "", "set", "--recipients", notificationsOwner+",, OWNER@example.com ,")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"email": map[string]any{"recipients": []any{notificationsOwner}}}, fake.lastUpdate(t),
		"blank and repeated addresses are dropped")

	_, err = executeNotificationsCommand(t, New(deps), "", "set", "--recipients=")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"email": map[string]any{"recipients": []any{}}}, fake.lastUpdate(t),
		"an empty --recipients emails the owner again")

	out, err := executeNotificationsCommand(t, New(deps), "", "set", "--remove-webhook")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"webhook": map[string]any{"url": ""}}, fake.lastUpdate(t))
	assert.Contains(t, out, "Webhook: not configured")
}

// Only the owner's account address can receive alerts. The CLI cannot tell
// which address that is, so the API refuses any other one, and its reason has
// to reach the user as is.
func TestNotificationsSetReportsTheOwnerOnlyRefusal(t *testing.T) {
	fake, deps := newNotificationsServer(t)

	// The request type validates addresses the way the local check does, so an
	// address the check accepts is sent; the server decides whether it takes it.
	out, err := executeNotificationsCommand(t, New(deps), "", "set", "--recipients", "dev@localhost,ops@[10.0.0.1]")
	require.Error(t, err)
	assert.Equal(t, map[string]any{"email": map[string]any{"recipients": []any{"dev@localhost", "ops@[10.0.0.1]"}}},
		fake.lastUpdate(t))
	assert.Contains(t, err.Error(), ownerOnlyRecipients)
	assert.NotContains(t, err.Error(), "{", "the reason is shown without the response body around it")
	assert.NotContains(t, out, "Notification settings saved")

	out, err = executeNotificationsCommand(t, New(deps), "", "get")
	require.NoError(t, err)
	assert.Contains(t, out, "Email: on, to the project owner", "a refused change saves nothing")
}

func TestNotificationsSetRefusesBeforeTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"nothing to change", nil, "nothing to change"},
		{"unknown policy", []string{"--notify-on", "always"}, "invalid --notify-on"},
		{"threshold too low", []string{"--threshold", "0"}, "--threshold must be between 1 and 100"},
		{"threshold too high", []string{"--threshold", "101"}, "--threshold must be between 1 and 100"},
		{"empty webhook", []string{"--webhook-url", ""}, "use --remove-webhook"},
		{"set and remove", []string{"--webhook-url", notificationsWebhook, "--remove-webhook"}, "none of the others can be"},
		{
			"too many recipients",
			[]string{"--recipients", "a@example.com,b@example.com,c@example.com,d@example.com,e@example.com," +
				"f@example.com,g@example.com,h@example.com,i@example.com,j@example.com,k@example.com"},
			"at most 10 recipients",
		},
		{
			"invalid recipients",
			[]string{"--recipients", "ops@example.com,oncall,Ops <ops@example.com>"},
			`not an email address: "oncall", "Ops <ops@example.com>"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, deps := newNotificationsServer(t)

			_, err := executeNotificationsCommand(t, New(deps), "", append([]string{"set"}, tc.args...)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Zero(t, fake.updateCount(), "a refused change never reaches the API")
		})
	}
}

func TestNotificationsRotateAsksFirst(t *testing.T) {
	fake, deps := newNotificationsServer(t)
	_, err := executeNotificationsCommand(t, New(deps), "", "set", "--webhook-url", notificationsWebhook)
	require.NoError(t, err)

	out, err := executeNotificationsCommand(t, New(deps), "n\n", "rotate-webhook-secret")
	require.NoError(t, err)
	assert.Contains(t, out, "will reject deliveries until it has the new one")
	assert.Contains(t, out, "Cancelled.")
	assert.Zero(t, fake.rotated, "a declined rotation sends nothing")

	out, err = executeNotificationsCommand(t, New(deps), "y\n", "rotate-webhook-secret")
	require.NoError(t, err)
	assert.Equal(t, 1, fake.rotated)
	assert.Contains(t, out, notificationsRotatedSecret)
}

// A script that answers the prompt still reads only the settings, and with
// them the new secret, from stdout.
func TestNotificationsRotateJSONKeepsThePromptOffStdout(t *testing.T) {
	_, deps := newNotificationsServer(t)
	_, err := executeNotificationsCommand(t, New(deps), "", "set", "--webhook-url", notificationsWebhook)
	require.NoError(t, err)

	cmd := New(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetArgs([]string{"rotate-webhook-secret", "--json"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stderr.String(), "Rotate the webhook signing secret?")
	var settings map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &settings), "stdout is only JSON: %s", stdout.String())
	webhook, _ := settings["webhook"].(map[string]any)
	assert.Equal(t, notificationsRotatedSecret, webhook["secret"])
}

func TestNotificationsRotateWithoutAWebhookSaysHowToSetOne(t *testing.T) {
	_, deps := newNotificationsServer(t)

	_, err := executeNotificationsCommand(t, New(deps), "", "rotate-webhook-secret", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no webhook is configured")
	assert.Contains(t, err.Error(), "notifications set --webhook-url <url>")
}

func TestNotificationsTestFailsWhenADeliveryDidNotArrive(t *testing.T) {
	fake, deps := newNotificationsServer(t)
	fake.status = http.StatusOK
	fake.test = map[string]any{"deliveries": []any{
		map[string]any{"channel": "email", "delivered": true},
		map[string]any{"channel": "webhook", "delivered": false, "status_code": 500, "error": "webhook responded 500"},
	}}

	out, err := executeNotificationsCommand(t, New(deps), "", "test")
	require.Error(t, err, "a delivery that did not arrive fails the command")
	assert.Contains(t, out, "Test email delivered")
	assert.Equal(t, "test webhook not delivered (HTTP 500): webhook responded 500", err.Error())
	var marked *sensitive.Error
	require.ErrorAs(t, err, &marked, "a delivery's reason stays out of telemetry")

	fake.test = map[string]any{"deliveries": []any{
		map[string]any{"channel": "webhook", "delivered": true, "status_code": 204},
	}}
	out, err = executeNotificationsCommand(t, New(deps), "", "test")
	require.NoError(t, err)
	assert.Contains(t, out, "Test webhook delivered (HTTP 204)")
}

func TestNotificationsTestReportsTheAPIsRefusal(t *testing.T) {
	fake, deps := newNotificationsServer(t)
	fake.status = http.StatusTooManyRequests
	fake.test = map[string]any{"error": "a test notification was sent less than a minute ago"}

	_, err := executeNotificationsCommand(t, New(deps), "", "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "less than a minute ago")
}
