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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/sensitive"
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
		"extra brace":   `{"category":"cli_bug"}}`,
		"extra bracket": `{"category":"cli_bug"}]`,
		"not an object": `["cli_bug"]`,
		"wrong type":    `{"commands":"volcano deploy"}`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseInput(strings.NewReader(body))
			require.Error(t, err)
		})
	}

	_, err = ParseInput(strings.NewReader(`{"acme-customer":1}`))
	assert.NotEmpty(t, sensitive.Text(err), "the decoder quotes the key, so usage telemetry must not send it")

	for body, want := range map[string]string{
		`{"category":"cli_bug","details":"y","Details":"z"}`: `parse input: unknown field "Details"; keys are case-sensitive`,
		`{"Summary":"a"}`: `parse input: unknown field "Summary"`,
		`{"summary":"a","details":"d","summary":"b"}`:       `parse input: field "summary" appears more than once`,
		`{"commands":["a"],"commands":["b"],"summary":"s"}`: `parse input: field "commands" appears more than once`,
	} {
		_, err := ParseInput(strings.NewReader(body))
		require.ErrorContains(t, err, want, body)
		assert.NotEmpty(t, sensitive.Text(err), "the error quotes the key, so usage telemetry must not send it")
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

	r, err := Build(in, client, "/home/dev", now)
	require.NoError(t, err)
	id, err := uuid.Parse(r.FeedbackID)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), id.Version(), "the server reads the time from a UUIDv7")
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

	anonymous, err := Build(validInput(), telemetry.Client{Anonymous: true}, "", now)
	require.NoError(t, err)
	assert.True(t, anonymous.Anonymous)

	report, err := Build(validInput(), telemetry.Client{}, "", now)
	require.NoError(t, err)
	payload, err := json.Marshal(report)
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

func TestSend_SharesTheAPITransport(t *testing.T) {
	api.ResetLastInstructionsForTest()
	t.Cleanup(api.ResetLastInstructionsForTest)
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("X-Volcano-Device-Instruction", api.DeviceInstructionReauth)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.Client(), srv.URL, "tok", []byte(`{}`))
	require.Error(t, err)
	assert.Equal(t, http.StatusUnauthorized, api.Status(err))
	assert.NotEmpty(t, got.Header.Get("X-Volcano-CLI-Version"), "the version protocol reports the CLI version")
	assert.Contains(t, got.Header.Get("User-Agent"), "volcano")
	assert.Equal(t, api.DeviceInstructionReauth, api.LastInstructions().DeviceInstruction, "main prints the login hint")
}

func TestSend_UnreachableNamesTheAPI(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	err := Send(context.Background(), nil, url, "tok", []byte(`{}`))
	require.ErrorContains(t, err, "cannot reach the Volcano API")
}

func TestSend_KeepsServerMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"summary is over 120 characters"}`))
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.Client(), srv.URL, "", []byte(`{}`))
	require.EqualError(t, err, "feedback endpoint returned HTTP 422: summary is over 120 characters")
	assert.NotEmpty(t, sensitive.Text(err), "the message can quote the report")
}

func TestSend_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.Client(), srv.URL, "", []byte(`{}`))
	require.EqualError(t, err, "feedback endpoint returned HTTP 404: Not Found")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.ErrorContains(t, Send(context.Background(), srv.Client(), srv.URL, "", make([]byte, maxPayloadBytes+1)), "over")
}
