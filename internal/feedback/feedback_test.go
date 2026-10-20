package feedback

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/telemetry"
)

func validInput() Input {
	return Input{Category: "cli_bug", Summary: "s", Details: "d"}
}

func TestValidate(t *testing.T) {
	require.NoError(t, validInput().Validate())

	err := Input{Category: "nope", Summary: " ", Details: strings.Repeat("x", MaxDetails+1)}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "category must be one of cli_bug, docs_mismatch")
	assert.Contains(t, err.Error(), "summary is required")
	assert.Contains(t, err.Error(), "details is 4001 characters; the limit is 4000")

	in := validInput()
	in.Commands = make([]string, MaxCommands+1)
	require.ErrorContains(t, in.Validate(), "commands has 11 entries")

	in = validInput()
	in.Commands = []string{strings.Repeat("é", MaxCommand+1)}
	assert.ErrorContains(t, in.Validate(), "a command is 501 characters")
}

func TestParseInput(t *testing.T) {
	in, err := ParseInput(strings.NewReader(`{"category":"cli_bug","summary":"s","details":"line 1\nline 2","commands":["volcano deploy"]}`))
	require.NoError(t, err)
	assert.Equal(t, Input{Category: "cli_bug", Summary: "s", Details: "line 1\nline 2", Commands: []string{"volcano deploy"}}, in)

	for name, body := range map[string]string{
		"unknown key":   `{"category":"cli_bug","title":"s"}`,
		"two objects":   `{"category":"cli_bug"} {}`,
		"not an object": `["cli_bug"]`,
		"wrong type":    `{"commands":"volcano deploy"}`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseInput(strings.NewReader(body))
			require.Error(t, err)
		})
	}

	_, err = ParseInput(strings.NewReader(strings.Repeat(" ", maxPayloadBytes+1)))
	require.ErrorContains(t, err, "input is over")
}

func TestBuild(t *testing.T) {
	in := Input{
		Category:   "docs_mismatch",
		Summary:    "skill passes --json",
		Details:    "ran it in /home/dev/app\nwith Bearer abc",
		Actual:     "Error: unknown flag: --json",
		Workaround: "parsed the table",
		Commands:   []string{"volcano functions list --json", "  "},
	}
	client := telemetry.Client{DeviceID: "d", CLIVersion: "v1.2.3", OS: "darwin", Arch: "arm64", Agent: "codex", AgentSource: "marker", ProjectID: "p"}
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.FixedZone("x", 3600))

	r := Build(in, client, "/home/dev", now)
	assert.NotEmpty(t, r.FeedbackID)
	r.FeedbackID = ""
	assert.Equal(t, Report{
		DeviceID:    "d",
		Timestamp:   "2026-10-15T02:00:00Z",
		Category:    "docs_mismatch",
		Summary:     "skill passes --json",
		Details:     "ran it in ~/app\nwith Bearer [redacted]",
		Actual:      "Error: unknown flag: --json",
		Workaround:  "parsed the table",
		Commands:    []string{"volcano functions list --json"},
		CLIVersion:  "v1.2.3",
		OS:          "darwin",
		Arch:        "arm64",
		Agent:       "codex",
		AgentSource: "marker",
		ProjectID:   "p",
	}, r)

	payload, err := json.Marshal(Build(validInput(), telemetry.Client{}, "", now))
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"commands":[]`, "commands is always a list")
	assert.NotContains(t, string(payload), "expected")
}

func TestSend(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	require.NoError(t, Send(context.Background(), srv.Client(), srv.URL+"/", "tok", []byte(`{"a":1}`)))
	assert.Equal(t, http.MethodPost, got.Method)
	assert.Equal(t, "/cli/feedback", got.URL.Path)
	assert.Equal(t, "Bearer tok", got.Header.Get("Authorization"))
	assert.Equal(t, "application/json", got.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"a":1}`, string(body))

	require.NoError(t, Send(context.Background(), srv.Client(), srv.URL, "", []byte(`{}`)))
	assert.Empty(t, got.Header.Get("Authorization"))
}

func TestSend_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	require.EqualError(t, Send(context.Background(), srv.Client(), srv.URL, "", []byte(`{}`)), "feedback endpoint returned HTTP 404")
	assert.ErrorContains(t, Send(context.Background(), srv.Client(), srv.URL, "", make([]byte, maxPayloadBytes+1)), "over")
}
