// Package feedback builds and sends the reports `volcano feedback` files: a
// coding agent's (or a person's) account of a problem it hit with Volcano, such
// as a CLI bug, docs that disagree with the CLI, or a missing command.
//
// A report is the agent's text plus the machine and agent fields a usage event
// carries (telemetry.Client). It is sent to POST /cli/feedback on the cloud API
// (volcano-hosting). The server joins a report to the usage events of the same
// device_id around its timestamp to find the failing command, so the CLI keeps
// no record of past runs. Feedback is off whenever telemetry is off.
package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/telemetry"
	"github.com/Kong/volcano-cli/internal/version"
)

// Categories are the kinds of problem a report can be about.
var Categories = []string{
	"cli_bug",
	"docs_mismatch",
	"missing_capability",
	"platform_error",
	"confusing_ux",
	"instruction_conflict",
}

// Length limits, in characters.
const (
	MaxSummary    = 120
	MaxDetails    = 4000
	MaxExpected   = 1000
	MaxActual     = 4000
	MaxWorkaround = 2000
	MaxCommand    = 500
	MaxCommands   = 10
)

// maxPayloadBytes is the server's body limit for POST /cli/feedback.
const maxPayloadBytes = 64 << 10

// sendTimeout bounds the one HTTP request.
const sendTimeout = 10 * time.Second

// Input is what the reporter wrote, from flags or from the JSON object
// `volcano feedback --input` reads.
type Input struct {
	Category   string   `json:"category"`
	Summary    string   `json:"summary"`
	Details    string   `json:"details"`
	Expected   string   `json:"expected"`
	Actual     string   `json:"actual"`
	Workaround string   `json:"workaround"`
	Commands   []string `json:"commands"`
}

// ParseInput reads one JSON object with Input's keys from r. Unknown keys and
// anything after the object are errors, so a misspelled key is not silently
// dropped.
func ParseInput(r io.Reader) (Input, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxPayloadBytes+1))
	if err != nil {
		return Input{}, err
	}
	if len(data) > maxPayloadBytes {
		return Input{}, fmt.Errorf("input is over %d bytes", maxPayloadBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var in Input
	if err := dec.Decode(&in); err != nil {
		return Input{}, fmt.Errorf("parse input: %w", err)
	}
	// More is false before a stray "}" or "]", so decode again and require
	// the end of the input.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Input{}, errors.New("parse input: expected one JSON object")
	}
	return in, nil
}

// Report is the request body of POST /cli/feedback. Field names are the wire
// contract with volcano-hosting; the server validates and redacts every value
// again.
type Report struct {
	// FeedbackID is generated here, so the CLI can print it without waiting
	// on the response and a retried send is recognizable as the same report.
	FeedbackID string   `json:"feedback_id"`
	DeviceID   string   `json:"device_id"`
	Timestamp  string   `json:"timestamp"`
	Category   string   `json:"category"`
	Summary    string   `json:"summary"`
	Details    string   `json:"details"`
	Expected   string   `json:"expected,omitempty"`
	Actual     string   `json:"actual,omitempty"`
	Workaround string   `json:"workaround,omitempty"`
	Commands   []string `json:"commands"`

	CLIVersion      string `json:"cli_version"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	Agent           string `json:"agent"`
	AgentSource     string `json:"agent_source"`
	PluginInstalled bool   `json:"plugin_installed"`
	PluginVersion   string `json:"plugin_version"`
	ProjectID       string `json:"project_id,omitempty"`
}

// Validate checks the category, the required fields, and the length limits.
// Limits are checked on the text as written, so the reporter can shorten it.
func (in Input) Validate() error {
	var errs []error
	if !slices.Contains(Categories, in.Category) {
		errs = append(errs, fmt.Errorf("category must be one of %s", strings.Join(Categories, ", ")))
	}
	for _, f := range []struct {
		name     string
		value    string
		max      int
		required bool
	}{
		{"summary", in.Summary, MaxSummary, true},
		{"details", in.Details, MaxDetails, true},
		{"expected", in.Expected, MaxExpected, false},
		{"actual", in.Actual, MaxActual, false},
		{"workaround", in.Workaround, MaxWorkaround, false},
	} {
		n := utf8.RuneCountInString(strings.TrimSpace(f.value))
		switch {
		case f.required && n == 0:
			errs = append(errs, fmt.Errorf("%s is required", f.name))
		case n > f.max:
			errs = append(errs, fmt.Errorf("%s is %d characters; the limit is %d", f.name, n, f.max))
		}
	}
	if len(in.Commands) > MaxCommands {
		errs = append(errs, fmt.Errorf("commands has %d entries; the limit is %d", len(in.Commands), MaxCommands))
	}
	for _, c := range in.Commands {
		if n := utf8.RuneCountInString(strings.TrimSpace(c)); n > MaxCommand {
			errs = append(errs, fmt.Errorf("a command is %d characters; the limit is %d", n, MaxCommand))
		}
	}
	return errors.Join(errs...)
}

// Build returns the report for in, with every text field passed through
// telemetry.ScrubText.
func Build(in Input, client telemetry.Client, home string, now time.Time) Report {
	scrub := func(s string) string { return telemetry.ScrubText(s, home) }
	commands := []string{}
	for _, c := range in.Commands {
		if c = scrub(c); c != "" {
			commands = append(commands, c)
		}
	}
	return Report{
		FeedbackID:      uuid.NewString(),
		DeviceID:        client.DeviceID,
		Timestamp:       now.UTC().Format(time.RFC3339),
		Category:        in.Category,
		Summary:         scrub(in.Summary),
		Details:         scrub(in.Details),
		Expected:        scrub(in.Expected),
		Actual:          scrub(in.Actual),
		Workaround:      scrub(in.Workaround),
		Commands:        commands,
		CLIVersion:      client.CLIVersion,
		OS:              client.OS,
		Arch:            client.Arch,
		Agent:           client.Agent,
		AgentSource:     client.AgentSource,
		PluginInstalled: client.PluginInstalled,
		PluginVersion:   client.PluginVersion,
		ProjectID:       client.ProjectID,
	}
}

// Send posts payload to apiURL's /cli/feedback, with token as a bearer when it
// is set. The server accepts a report without a token. A nil client uses one
// with a timeout.
func Send(ctx context.Context, client *http.Client, apiURL, token string, payload []byte) error {
	if len(payload) > maxPayloadBytes {
		return fmt.Errorf("feedback report is over %d bytes", maxPayloadBytes)
	}
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	url := strings.TrimRight(apiURL, "/") + "/cli/feedback"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "volcano-cli/"+version.Version)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("feedback endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}
