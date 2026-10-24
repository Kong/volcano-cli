package localmode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// localModeE2EMailpitURL is the local mail catcher's API, published to the
// host by the local-mode Compose file.
const localModeE2EMailpitURL = "http://localhost:8025"

// localModeE2ESelfWebhookURL is the local server itself, as the server dials
// it from inside its container. /health answers GET only, so a delivery there
// comes back 405: proof the webhook was posted, to an address the cloud
// refuses to save.
const localModeE2ESelfWebhookURL = "http://localhost:8000/health"

// requireLocalModeSendsNotifications is the cloud notifications round trip
// against the local stack: a test email lands in the local mail catcher, and
// a webhook may point at a private address, which only local mode allows.
func requireLocalModeSendsNotifications(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	t.Run("notifications", func(t *testing.T) {
		localModeSendsNotifications(t, binary, env, dir)
	})
}

func localModeSendsNotifications(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	defaults, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "notifications", "get")
	// This CLI merges before Kong/volcano-hosting#1965, and the lane runs the
	// local-nightly image, which serves notifications only once a nightly
	// includes that PR. Remove this once it does. Only a 404 without an API
	// message, the router's answer to an unknown route, is skipped; the API's
	// own 404s, such as "Project not found", say why.
	if err != nil && strings.Contains(defaults, "HTTP 404: Not Found") {
		t.Skipf("the local server image does not serve /projects/{id}/notifications yet (Kong/volcano-hosting#1965):\n%s", defaults)
	}
	if err != nil {
		t.Fatalf("volcano notifications get failed: %v\n%s", err, defaults)
	}
	requireContains(t, defaults, "on, to the project owner")
	requireContains(t, defaults, "not configured")
	requireContains(t, defaults, "3 failed scheduled runs in a row")

	secret := localModeE2EWebhookSecret(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"notifications", "set", "--webhook-url", localModeE2ESelfWebhookURL, "--json"))

	stored := runVolcanoLocalModeE2E(t, binary, env, dir, "notifications", "get")
	requireContains(t, stored, localModeE2ESelfWebhookURL)
	requireContains(t, stored, "configured")
	requireNotContains(t, stored, secret)

	sentAfter := time.Now().Add(-time.Minute)
	tested, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "notifications", "test")
	if err == nil {
		t.Fatalf("expected the test to fail on the webhook that answers 405\n%s", tested)
	}
	requireContains(t, tested, "Test email delivered")
	requireContains(t, tested, "test webhook not delivered (HTTP 405)")
	requireLocalModeE2EMailCaught(t, "Volcano test notification", sentAfter)

	rotatedSecret := localModeE2EWebhookSecret(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"notifications", "rotate-webhook-secret", "--yes", "--json"))
	if rotatedSecret == secret {
		t.Fatal("expected rotation to replace the webhook signing secret")
	}

	// The manifest describes the settings but leaves out the webhook, whose URL
	// can carry the receiver's token, and the secret.
	pulled := runVolcanoLocalModeE2E(t, binary, env, dir, "config", "pull", "--force")
	requireContains(t, pulled, "Configuration written to")
	manifestPath := filepath.Join("volcano", "volcano-config.yaml")
	manifest := readLocalModeE2EFile(t, dir, manifestPath)
	requireContains(t, manifest, "notifications:")
	requireNotContains(t, manifest, localModeE2ESelfWebhookURL)
	requireNotContains(t, manifest, rotatedSecret)
	removeLocalModeE2EFile(t, dir, manifestPath)

	removed := runVolcanoLocalModeE2E(t, binary, env, dir,
		"notifications", "set", "--remove-webhook", "--notify-on", "first_failure")
	requireContains(t, removed, "not configured")
	requireContains(t, removed, "a scheduler's first failed run")

	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "notifications", "rotate-webhook-secret", "--yes")
	if err == nil {
		t.Fatalf("expected rotating without a webhook to fail\n%s", output)
	}
	requireContains(t, output, "notifications set --webhook-url")
}

type localModeE2EMailpitSearch struct {
	Messages []struct {
		Subject string    `json:"Subject"`
		Created time.Time `json:"Created"`
		To      []struct {
			Address string `json:"Address"`
		} `json:"To"`
	} `json:"messages"`
}

// requireLocalModeE2EMailCaught waits for a message with subject in the local
// mail catcher, received after since and addressed to someone.
func requireLocalModeE2EMailCaught(t *testing.T, subject string, since time.Time) {
	t.Helper()

	query := url.Values{"query": {`subject:"` + subject + `"`}}
	endpoint := localModeE2EMailpitURL + "/api/v1/search?" + query.Encode()
	var last string
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		found, summary := localModeE2EMailpitHasMessage(t, endpoint, subject, since)
		if found {
			return
		}
		last = summary
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("no message %q reached the local mail catcher: %s", subject, last)
}

func localModeE2EMailpitHasMessage(t *testing.T, endpoint, subject string, since time.Time) (bool, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		t.Fatalf("failed to build the mail catcher request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, resp.Status
	}
	var search localModeE2EMailpitSearch
	if err := json.NewDecoder(resp.Body).Decode(&search); err != nil {
		return false, err.Error()
	}
	for _, message := range search.Messages {
		if strings.Contains(message.Subject, subject) && message.Created.After(since) && len(message.To) > 0 {
			return true, ""
		}
	}
	return false, fmt.Sprintf("%d message(s), none received since %s", len(search.Messages), since.Format(time.RFC3339))
}

// localModeE2EWebhookSecret reads the signing secret out of the --json
// settings a webhook change or rotation returns.
func localModeE2EWebhookSecret(t *testing.T, output string) string {
	t.Helper()

	var settings struct {
		Webhook struct {
			Secret string `json:"secret"`
		} `json:"webhook"`
	}
	if err := json.Unmarshal([]byte(output), &settings); err != nil {
		t.Fatalf("failed to decode the notification settings: %v\n%s", err, output)
	}
	if !strings.HasPrefix(settings.Webhook.Secret, "whsec_") {
		t.Fatal("expected a whsec_ webhook signing secret in the response")
	}
	return settings.Webhook.Secret
}
