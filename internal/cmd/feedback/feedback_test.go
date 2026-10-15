package feedback

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/feedback"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

var reportArgs = []string{
	"--category", "cli_bug",
	"--summary", "deploy exits 0 on failure",
	"--details", "deploy printed an error and exited 0",
	"--command", "volcano functions deploy --all",
	"--actual", "Error: build failed",
}

// setup gives the test a fresh home whose telemetry state is state, a fresh
// recorder, and a sink at VOLCANO_API_URL that collects the posted reports.
func setup(t *testing.T, state *config.TelemetryConfig) *[]feedback.Report {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(telemetry.EnvDisabled, "")
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	require.NoError(t, config.Update(func(cfg *config.Config) (bool, error) {
		cfg.Telemetry = state
		return true, nil
	}))
	SetRecorder(&telemetry.Recorder{})
	t.Cleanup(func() { SetRecorder(&telemetry.Recorder{}) })

	var reports []feedback.Report
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var report feedback.Report
		require.NoError(t, json.Unmarshal(body, &report))
		reports = append(reports, report)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("VOLCANO_API_URL", srv.URL)
	return &reports
}

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := New()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestFeedback_Sends(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	stdout, _, err := run(t, reportArgs...)
	require.NoError(t, err)
	require.Len(t, *reports, 1)
	r := (*reports)[0]
	assert.Equal(t, "Feedback sent. ID: "+r.FeedbackID+"\n", stdout)
	assert.Equal(t, "d", r.DeviceID)
	assert.Equal(t, "cli_bug", r.Category)
	assert.Equal(t, []string{"volcano functions deploy --all"}, r.Commands)
	assert.Equal(t, "Error: build failed", r.Actual)
}

func TestFeedback_TelemetryOffSendsNothing(t *testing.T) {
	for name, state := range map[string]*config.TelemetryConfig{
		"disabled":          {DeviceID: "d", NoticeShown: true, Disabled: true},
		"before the notice": nil,
	} {
		t.Run(name, func(t *testing.T) {
			reports := setup(t, state)
			stdout, stderr, err := run(t, reportArgs...)
			require.NoError(t, err, "exit 0, so an agent does not retry")
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, "Feedback not sent: telemetry is off")
			assert.Empty(t, *reports)
		})
	}

	t.Run("env opt-out", func(t *testing.T) {
		reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
		t.Setenv(telemetry.EnvDisabled, "1")
		_, stderr, err := run(t, reportArgs...)
		require.NoError(t, err)
		assert.Contains(t, stderr, "Feedback not sent")
		assert.Empty(t, *reports)
	})
}

func TestFeedback_DryRun(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	stdout, stderr, err := run(t, append(reportArgs, "--dry-run")...)
	require.NoError(t, err)
	assert.Empty(t, *reports)
	assert.Empty(t, stderr)
	var r feedback.Report
	require.NoError(t, json.Unmarshal([]byte(stdout), &r))
	assert.Equal(t, "d", r.DeviceID)
	assert.Equal(t, "deploy exits 0 on failure", r.Summary)
}

func TestFeedback_DryRunWithTelemetryOff(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true, Disabled: true})

	stdout, stderr, err := run(t, append(reportArgs, "--dry-run")...)
	require.NoError(t, err)
	assert.Contains(t, stdout, `"category": "cli_bug"`)
	assert.Contains(t, stderr, "would not be sent")
}

func TestFeedback_InvalidInput(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	_, _, err := run(t, "--category", "other", "--summary", "s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--category must be one of")
	assert.Contains(t, err.Error(), "--details is required")
	assert.Empty(t, *reports)
}

func TestFeedback_SendFailureFails(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("VOLCANO_API_URL", srv.URL)

	_, _, err := run(t, reportArgs...)
	assert.EqualError(t, err, "send feedback: feedback endpoint returned HTTP 404")
}
