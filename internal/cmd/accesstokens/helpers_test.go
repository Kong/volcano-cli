package accesstokens

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	accessTokenProjectID      = "22222222-2222-4222-8222-222222222222"
	accessTokenOtherProjectID = "33333333-3333-4333-8333-333333333333"
	accessTokenID             = "77777777-7777-4777-8777-777777777777"
)

func executeAccessTokenCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setAccessTokenCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveAccessTokenCommandTestConfig(t *testing.T, token string) {
	t.Helper()
	saveAccessTokenCommandTestConfigFor(t, token, accessTokenProjectID)
}

func saveAccessTokenCommandTestConfigFor(t *testing.T, token, projectID string) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: token,
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   projectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

// writeAccessTokenCommandJSON answers a request from the server's own
// goroutine, where t.FailNow — and so every require helper — is not valid. A
// failed encode is reported instead, and the test fails on its own goroutine.
func writeAccessTokenCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("failed to encode the access token response: %v", err)
	}
}

func accessTokenCommandPayload(id, name string) map[string]any {
	return map[string]any{
		"all_time_requests": 42,
		"created_at":        time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
		"id":                id,
		"last_used_at":      time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339),
		"name":              name,
		"project_id":        accessTokenProjectID,
		"scope":             "full",
		"status":            "active",
		"token_prefix":      "pt-Wq9l2m4X",
		"token_source":      "cli",
	}
}

// accessTokenCommandUsagePayload mirrors the API's usage shape: days is the
// window size, daily the zero-filled series, total_requests the window total.
func accessTokenCommandUsagePayload(id, name string, requests ...int) map[string]any {
	daily := make([]any, 0, len(requests))
	total := 0
	for i, count := range requests {
		daily = append(daily, map[string]any{
			"day":      time.Date(2026, 9, 14+i, 0, 0, 0, 0, time.UTC).Format(time.DateOnly),
			"requests": count,
		})
		total += count
	}
	return map[string]any{
		"token_id":       id,
		"name":           name,
		"days":           len(requests),
		"daily":          daily,
		"total_requests": total,
	}
}

func accessTokenCommandPage(tokens ...map[string]any) map[string]any {
	return map[string]any{
		"data":     tokens,
		"has_more": false,
		"page":     1,
		"limit":    100,
		"total":    len(tokens),
	}
}
