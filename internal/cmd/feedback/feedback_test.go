package feedback

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/feedback"
	"github.com/Kong/volcano-cli/internal/sensitive"
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
// The first-run notice counts as shown for feedback unless state is nil.
func setup(t *testing.T, state *config.TelemetryConfig) *[]feedback.Report {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(telemetry.EnvDisabled, "")
	t.Setenv(telemetry.EnvFeedbackDisabled, "")
	t.Setenv(telemetry.EnvFeedbackDebug, "")
	t.Setenv("VOLCANO_TOKEN", "tok")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	require.NoError(t, config.Update(func(cfg *config.Config) (bool, error) {
		cfg.Telemetry = state
		if state != nil {
			cfg.Feedback = &config.FeedbackConfig{NoticeShown: true}
		}
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
	return runWithStdin(t, "", args...)
}

func runWithStdin(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := New()
	var out, errOut bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
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
	assert.False(t, r.Anonymous, "telemetry is on")
	assert.Equal(t, "cli_bug", r.Category)
	assert.Equal(t, []string{"volcano functions deploy --all"}, r.Commands)
	assert.Equal(t, "Error: build failed", r.Actual)
}

func TestFeedback_DebugSavesLocally(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv(telemetry.EnvFeedbackDebug, "1")

	for range 2 {
		stdout, _, err := run(t, reportArgs...)
		require.NoError(t, err)
		assert.Contains(t, stdout, "Feedback sent. ID: ")
	}
	assert.Empty(t, *reports, "nothing is sent")

	data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".volcano", "feedback.jsonl"))
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	require.Len(t, lines, 2, "each report is appended as one line")
	var r feedback.Report
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &r))
	assert.Equal(t, "d", r.DeviceID)
	assert.Equal(t, "cli_bug", r.Category)
}

func TestFeedback_OffSendsNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		state *config.TelemetryConfig
		setup func(t *testing.T)
	}{
		"disabled": {&config.TelemetryConfig{DeviceID: "d", NoticeShown: true}, func(t *testing.T) {
			t.Helper()
			_, _, err := run(t, "disable")
			require.NoError(t, err)
		}},
		"env opt-out": {&config.TelemetryConfig{DeviceID: "d", NoticeShown: true}, func(t *testing.T) {
			t.Helper()
			t.Setenv(telemetry.EnvFeedbackDisabled, "1")
		}},
		"before the notice": {nil, func(*testing.T) {}},
	} {
		t.Run(name, func(t *testing.T) {
			reports := setup(t, tc.state)
			tc.setup(t)
			stdout, stderr, err := run(t, reportArgs...)
			require.NoError(t, err, "exit 0, so an agent does not retry")
			assert.Empty(t, stdout)
			assert.Empty(t, stderr, "nothing is printed")
			assert.Empty(t, *reports)
		})
	}
}

// telemetryOff turns telemetry off in the config or by env.
var telemetryOff = map[string]func(t *testing.T){
	"disabled": func(t *testing.T) {
		t.Helper()
		require.NoError(t, config.Update(func(cfg *config.Config) (bool, error) {
			cfg.Telemetry.Disabled = true
			return true, nil
		}))
	},
	"env opt-out": func(t *testing.T) {
		t.Helper()
		t.Setenv(telemetry.EnvDisabled, "1")
	},
}

func TestFeedback_TelemetryOffTurnsFeedbackOff(t *testing.T) {
	for name, setTelemetryOff := range telemetryOff {
		t.Run(name, func(t *testing.T) {
			reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
			setTelemetryOff(t)

			stdout, stderr, err := run(t, reportArgs...)
			require.NoError(t, err, "exit 0, so an agent does not retry")
			assert.Empty(t, stdout)
			assert.Empty(t, stderr)
			assert.Empty(t, *reports)

			stdout, _, err = run(t, "status")
			require.NoError(t, err)
			assert.Equal(t, "Feedback is disabled because telemetry is off. Run `volcano feedback enable` to turn it on.\n", stdout)
		})
	}
}

func TestFeedback_EnabledWithTelemetryOffSendsAnonymously(t *testing.T) {
	for name, setTelemetryOff := range telemetryOff {
		t.Run(name, func(t *testing.T) {
			setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
			setTelemetryOff(t)
			_, _, err := run(t, "enable")
			require.NoError(t, err)
			SetRecorder(&telemetry.Recorder{}) // the next process reads the setting
			t.Setenv("VOLCANO_TOKEN", "tok")
			t.Setenv("VOLCANO_PROJECT_ID", "p-env")
			var auth []string
			var body []byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth = r.Header.Values("Authorization")
				body, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()
			t.Setenv("VOLCANO_API_URL", srv.URL)

			stdout, _, err := run(t, "status")
			require.NoError(t, err)
			assert.Equal(t, "Feedback is enabled. Run `volcano feedback disable` to turn it off.\n", stdout)

			_, _, err = run(t, reportArgs...)
			require.NoError(t, err)
			require.NotEmpty(t, body, "sent while telemetry is off")
			assert.Equal(t, []string{"Bearer tok"}, auth, "the server still checks the login")
			var fields map[string]any
			require.NoError(t, json.Unmarshal(body, &fields))
			assert.Equal(t, true, fields["anonymous"])
			assert.NotContains(t, fields, "device_id")
			assert.NotContains(t, fields, "project_id", "a project belongs to one account")
			assert.Equal(t, "cli_bug", fields["category"])
		})
	}
}

func TestFeedback_StatusEnableDisable(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	stdout, _, err := run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled. Run `volcano feedback disable` to turn it off.\n", stdout)

	stdout, _, err = run(t, "disable")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is disabled.\n", stdout)
	cfg, err := config.LoadWithoutLink()
	require.NoError(t, err)
	require.NotNil(t, cfg.Feedback.Enabled)
	assert.False(t, *cfg.Feedback.Enabled)
	assert.False(t, cfg.Telemetry.Disabled, "telemetry is unchanged")

	stdout, _, err = run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is disabled. Run `volcano feedback enable` to turn it on.\n", stdout)

	t.Setenv(telemetry.EnvFeedbackDisabled, "1")
	stdout, stderr, err := run(t, "enable")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled.\n", stdout)
	assert.Contains(t, stderr, "VOLCANO_FEEDBACK_DISABLED is set")
	stdout, _, err = run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is disabled by VOLCANO_FEEDBACK_DISABLED.\n", stdout)
}

func TestFeedback_StatusOnAFreshMachineShowsTheNotice(t *testing.T) {
	reports := setup(t, nil)

	stdout, stderr, err := run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled. Run `volcano feedback disable` to turn it off.\n", stdout)
	assert.Contains(t, stderr, "Coding agents can also send feedback reports", "status shows the first-run notice")
	cfg, err := config.LoadWithoutLink()
	require.NoError(t, err)
	require.NotNil(t, cfg.Feedback)
	assert.True(t, cfg.Feedback.NoticeShown)

	SetRecorder(&telemetry.Recorder{}) // the next process
	stdout, _, err = run(t, reportArgs...)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Feedback sent. ID: ")
	assert.Len(t, *reports, 1, "the report after status is sent")
}

func TestFeedback_StatusInAReadOnlyHome(t *testing.T) {
	reports := setup(t, nil)
	// A fresh home where ~/.volcano cannot be created, as in a read-only
	// mount.
	home := t.TempDir()
	require.NoError(t, os.Chmod(home, 0o500))
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
	t.Setenv("HOME", home)

	stdout, stderr, err := run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled, but no report is sent until the first-run notice can be saved to ~/.volcano/config.json.\n", stdout)
	assert.Empty(t, stderr, "no notice that could not be saved")

	SetRecorder(&telemetry.Recorder{})
	stdout, _, err = run(t, reportArgs...)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	assert.Empty(t, *reports)
}

func TestFeedback_LoggedOutSendsNothing(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv("VOLCANO_TOKEN", "")

	stdout, stderr, err := runWithStdin(t, "not json", "--input", "-")
	require.NoError(t, err, "exit 0, so an agent does not retry")
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Empty(t, *reports)

	stdout, _, err = run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled, but no report is sent until you log in with `volcano login`.\n", stdout)

	_, stderr, err = run(t, append([]string{"--dry-run"}, reportArgs...)...)
	require.NoError(t, err)
	assert.Contains(t, stderr, "This report would not be sent.")
}

func TestFeedback_ProjectTokenSendsNothing(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv("VOLCANO_TOKEN", "pt-abc")

	stdout, stderr, err := run(t, reportArgs...)
	require.NoError(t, err, "exit 0, so an agent does not retry")
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Empty(t, *reports)

	stdout, _, err = run(t, "status")
	require.NoError(t, err)
	assert.Equal(t, "Feedback is enabled, but a project access token cannot send reports. Log in with `volcano login`.\n", stdout)

	_, stderr, err = run(t, append([]string{"--dry-run"}, reportArgs...)...)
	require.NoError(t, err)
	assert.Contains(t, stderr, "This report would not be sent.")
}

func TestFeedback_SettingSurvivesLogout(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	_, _, err := run(t, "disable")
	require.NoError(t, err)

	require.NoError(t, config.ClearLogin())
	cfg, err := config.LoadWithoutLink()
	require.NoError(t, err)
	require.NotNil(t, cfg.Feedback)
	require.NotNil(t, cfg.Feedback.Enabled)
	assert.False(t, *cfg.Feedback.Enabled)
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

func TestFeedback_DryRunKeepsPlaceholders(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	stdout, _, err := run(t, "--dry-run", "--input",
		`{"category":"cli_bug","summary":"deploy of <function> fails","details":"see https://x.dev/a?b=1&c=2"}`)
	require.NoError(t, err)
	assert.Contains(t, stdout, `"summary": "deploy of <function> fails"`)
	assert.NotContains(t, stdout, `\u003c`)
}

func TestFeedback_DryRunWithFeedbackOff(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv(telemetry.EnvFeedbackDisabled, "1")

	stdout, stderr, err := run(t, append(reportArgs, "--dry-run")...)
	require.NoError(t, err)
	assert.Contains(t, stdout, `"category": "cli_bug"`)
	assert.Contains(t, stderr, "would not be sent")
}

func TestFeedback_OffSkipsTheReport(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv(telemetry.EnvFeedbackDisabled, "1")

	stdout, stderr, err := runWithStdin(t, "not json", "--input", "-")
	require.NoError(t, err, "an invalid report does not fail while feedback is off")
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
	assert.Empty(t, *reports)
}

func TestFeedback_StrayArgumentIsSensitive(t *testing.T) {
	setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	_, _, err := run(t, "deploy of acme-billing fails")
	require.ErrorContains(t, err, "acme-billing")
	assert.NotEmpty(t, sensitive.Text(err), "usage telemetry must not send report text")
}

func TestFeedback_UnreadableConfigFails(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	path, err := config.Path()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))

	_, _, err = run(t, reportArgs...)
	require.Error(t, err)
	assert.Empty(t, *reports)
}

func TestFeedback_ProjectEnvIsNotAUse(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	t.Setenv("VOLCANO_PROJECT_ID", "p-env")
	config.ConsumeProjectEnvUse()

	_, _, err := run(t, reportArgs...)
	require.NoError(t, err)
	require.Len(t, *reports, 1)
	assert.Equal(t, "p-env", (*reports)[0].ProjectID)
	assert.False(t, config.ConsumeProjectEnvUse(), "no deprecation warning for a project feedback did not run against")
}

func TestFeedback_LimitsApplyAfterRedaction(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	summary := strings.Repeat("x", feedback.MaxSummary-7) + " a@b.io"

	_, _, err := run(t, "--category", "cli_bug", "--summary", summary, "--details", "d")
	require.ErrorContains(t, err, "after redaction, summary is 124 characters")
	assert.Empty(t, *reports)
}

func TestFeedback_InvalidInput(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	_, _, err := run(t, "--category", "other", "--summary", "s")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "category must be one of")
	assert.Contains(t, err.Error(), "details is required")
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
	assert.EqualError(t, err, "send feedback: feedback endpoint returned HTTP 404: Not Found")
}

const inputJSON = `{
  "category": "missing_capability",
  "summary": "no command sets a function's timeout",
  "details": "Needed a longer timeout.\nEdited volcano-config.yaml by hand.",
  "commands": ["volcano functions update hello --timeout 60"],
  "workaround": "edited the manifest"
}`

func TestFeedback_InputFromStdin(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	stdout, _, err := runWithStdin(t, inputJSON, "--input", "-")
	require.NoError(t, err)
	require.Len(t, *reports, 1)
	r := (*reports)[0]
	assert.Contains(t, stdout, r.FeedbackID)
	assert.Equal(t, "missing_capability", r.Category)
	assert.Equal(t, "Needed a longer timeout.\nEdited volcano-config.yaml by hand.", r.Details)
	assert.Equal(t, []string{"volcano functions update hello --timeout 60"}, r.Commands)
	assert.Equal(t, "d", r.DeviceID)
}

func TestFeedback_InputInline(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	_, _, err := run(t, "--input", `  {"category":"cli_bug","summary":"s","details":"can\u0027t deploy"}`)
	require.NoError(t, err)
	require.Len(t, *reports, 1)
	assert.Equal(t, "can't deploy", (*reports)[0].Details)
}

func TestFeedback_InputFromFile(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte(inputJSON), 0o600))

	_, _, err := run(t, "--input", path, "--dry-run")
	require.NoError(t, err)
	assert.Empty(t, *reports)

	_, _, err = run(t, "--input", path)
	require.NoError(t, err)
	assert.Len(t, *reports, 1)
}

func TestFeedback_InputErrors(t *testing.T) {
	reports := setup(t, &config.TelemetryConfig{DeviceID: "d", NoticeShown: true})

	_, _, err := runWithStdin(t, `{"category":"cli_bug","title":"s"}`, "--input", "-")
	require.ErrorContains(t, err, `unknown field "title"`)

	_, _, err = runWithStdin(t, `{"category":"cli_bug"}`, "--input", "-")
	require.ErrorContains(t, err, "summary is required")

	_, _, err = runWithStdin(t, inputJSON, "--input", "-", "--summary", "x")
	require.ErrorContains(t, err, "none of the others can be")

	_, _, err = run(t, "--input", "deploy failed: can't find build output")
	require.ErrorContains(t, err, "read input: open deploy failed")
	assert.NotEmpty(t, sensitive.Text(err), "report text given as a path stays out of usage telemetry")

	assert.Empty(t, *reports)
}
