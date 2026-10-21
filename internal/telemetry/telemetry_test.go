package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sensitive"
	"github.com/Kong/volcano-cli/internal/version"
)

// memConfig is an in-memory config.json. load returns a copy, as reading the
// file would.
type memConfig struct {
	cfg      config.Config
	saves    int
	failSave bool
	failLoad bool
}

func (m *memConfig) load() (*config.Config, error) {
	if m.failLoad {
		return nil, errors.New("parse config.json")
	}
	cfg := m.cfg
	// The real VOLCANO_PROJECT_ID must not leak into events built in tests.
	cfg.IgnoreEnv = true
	if m.cfg.Telemetry != nil {
		state := *m.cfg.Telemetry
		cfg.Telemetry = &state
	}
	if m.cfg.Feedback != nil {
		feedback := *m.cfg.Feedback
		cfg.Feedback = &feedback
	}
	return &cfg, nil
}

func (m *memConfig) update(fn func(*config.Config) (bool, error)) error {
	if m.failSave {
		return errors.New("read-only home")
	}
	cfg, err := m.load()
	if err != nil {
		return err
	}
	changed, err := fn(cfg)
	if err != nil || !changed {
		return err
	}
	m.cfg = *cfg
	m.saves++
	return nil
}

// state is the saved telemetry block.
func (m *memConfig) state() State {
	if m.cfg.Telemetry == nil {
		return State{}
	}
	return *m.cfg.Telemetry
}

func (m *memConfig) setState(state State) { m.cfg.Telemetry = &state }

type harness struct {
	env     map[string]string
	store   *memConfig
	home    string
	sent    [][]byte
	plugins map[string]string // harness -> version; present means installed
	now     time.Time
}

// newHarness returns a fresh machine where feedback was already announced, so
// tests of usage data see only telemetry's notice. Tests of the feedback
// notice clear store.cfg.Feedback.
func newHarness() *harness {
	return &harness{
		env:     map[string]string{},
		store:   &memConfig{cfg: config.Config{Feedback: &config.FeedbackConfig{NoticeShown: true}}},
		home:    "/home/dev",
		plugins: map[string]string{},
		now:     time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC),
	}
}

// recorder returns a fresh Recorder, as a new CLI process would have.
func (h *harness) recorder() *Recorder {
	return &Recorder{
		Getenv: func(k string) string { return h.env[k] },
		LookupEnv: func(k string) (string, bool) {
			v, ok := h.env[k]
			return v, ok
		},
		Now:          func() time.Time { return h.now },
		LoadConfig:   h.store.load,
		UpdateConfig: h.store.update,
		HomeDir:      func() (string, error) { return h.home, nil },
		Send:         func(p []byte) error { h.sent = append(h.sent, p); return nil },
		PluginStatus: func(name string) (bool, string) {
			v, ok := h.plugins[name]
			return ok, v
		},
	}
}

func (h *harness) lastEvent(t *testing.T) Event {
	t.Helper()
	require.NotEmpty(t, h.sent)
	var e Event
	require.NoError(t, json.Unmarshal(h.sent[len(h.sent)-1], &e))
	return e
}

// commandTree returns the leaf command of `volcano cloud functions deploy`
// after parsing args, so Flags().Visit sees what was set.
func commandTree(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "volcano"}
	cloud := &cobra.Command{Use: "cloud"}
	functions := &cobra.Command{Use: "functions"}
	deploy := &cobra.Command{Use: "deploy", RunE: func(*cobra.Command, []string) error { return nil }}
	deploy.Flags().Bool("all", false, "")
	deploy.Flags().String("name", "", "")
	root.PersistentFlags().Bool("debug", false, "")
	functions.AddCommand(deploy)
	cloud.AddCommand(functions)
	root.AddCommand(cloud)
	root.SetArgs(append([]string{"cloud", "functions", "deploy"}, args...))
	cmd, err := root.ExecuteC()
	require.NoError(t, err)
	return cmd
}

func TestRecord_FirstRunPrintsNoticeAndSendsNothing(t *testing.T) {
	h := newHarness()
	var stderr bytes.Buffer

	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Contains(t, stderr.String(), "collects usage data")
	assert.Contains(t, stderr.String(), "volcano telemetry disable")
	assert.Contains(t, stderr.String(), "volcano telemetry --help")
	assert.NotContains(t, stderr.String(), "volcano feedback", "feedback was already announced")
	assert.Empty(t, h.sent, "the run that shows the notice sends nothing")
	assert.True(t, h.store.state().NoticeShown)
	deviceID := h.store.state().DeviceID
	assert.NotEmpty(t, deviceID)

	stderr.Reset()
	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, stderr.String())
	require.Len(t, h.sent, 1)
	assert.Equal(t, deviceID, h.lastEvent(t).DeviceID)
	assert.Equal(t, 1, h.store.saves)
}

func TestRecord_FreshMachineAnnouncesBoth(t *testing.T) {
	h := newHarness()
	h.store.cfg.Feedback = nil
	var stderr bytes.Buffer

	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Contains(t, stderr.String(), "collects usage data")
	assert.Contains(t, stderr.String(), "volcano telemetry disable")
	assert.Contains(t, stderr.String(), "volcano feedback disable")
	assert.True(t, h.store.state().NoticeShown)
	assert.True(t, h.store.cfg.Feedback.NoticeShown)
	assert.Empty(t, h.sent)
}

func TestRecord_UnwritableHomeKeepsSendingAnnouncedTelemetry(t *testing.T) {
	// Saved by a CLI from before feedback, then mounted read-only.
	h := newHarness()
	h.store.cfg.Feedback = nil
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.store.failSave = true
	var stderr bytes.Buffer

	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, stderr.String(), "a notice that can never be marked shown would print on every run")
	require.Len(t, h.sent, 1)
	assert.Equal(t, "d", h.lastEvent(t).DeviceID)
	assert.False(t, h.recorder().FeedbackOn())
}

func TestRecord_UnwritableStatePrintsNoNoticeAndSendsNothing(t *testing.T) {
	h := newHarness()
	h.store.failSave = true
	var stderr bytes.Buffer

	for range 3 {
		h.recorder().Record(Invocation{Stderr: &stderr})
	}
	assert.Empty(t, stderr.String(), "a notice that can never be marked shown would print on every run")
	assert.Empty(t, h.sent)
}

func TestRecord_SkipsCompletionAndOffline(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})

	complete := &cobra.Command{Use: cobra.ShellCompRequestCmd}
	h.recorder().Record(Invocation{Command: complete})
	completeNoDesc := &cobra.Command{Use: cobra.ShellCompNoDescRequestCmd}
	h.recorder().Record(Invocation{Command: completeNoDesc})

	docs := &cobra.Command{Use: "list", RunE: func(*cobra.Command, []string) error { return nil }}
	docs.Flags().Bool("offline", false, "")
	require.NoError(t, docs.ParseFlags([]string{"--offline"}))
	h.recorder().Record(Invocation{Command: docs})
	assert.Empty(t, h.sent)

	online := &cobra.Command{Use: "list"}
	online.Flags().Bool("offline", false, "")
	h.recorder().Record(Invocation{Command: online})
	assert.Len(t, h.sent, 1, "--offline unset still records")
}

func TestRecord_FirstRunKeepsConcurrentOptOut(t *testing.T) {
	h := newHarness()
	r := h.recorder()
	require.False(t, r.Reporting(), "the process starts before the opt-out")
	// `volcano telemetry disable` in another process, while this one runs.
	h.store.setState(State{Disabled: true})
	var stderr bytes.Buffer

	r.Record(Invocation{Stderr: &stderr})

	assert.Equal(t, State{Disabled: true}, h.store.state(), "the first-run write must not clobber the opt-out")
	assert.Empty(t, stderr.String())
	assert.Empty(t, h.sent)
}

func TestRecord_ConcurrentFirstRunSendsNothing(t *testing.T) {
	h := newHarness()
	r := h.recorder()
	require.False(t, r.Reporting())
	// Another process shows the notice while this one runs.
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	var stderr bytes.Buffer

	r.Record(Invocation{Stderr: &stderr})

	assert.Empty(t, h.sent, "this process started before the notice")
	assert.Empty(t, stderr.String(), "the other process printed the notice")
}

func TestRecord_MissingDeviceIDAfterNoticeStillSends(t *testing.T) {
	h := newHarness()
	h.store.setState(State{NoticeShown: true}) // `volcano telemetry enable` on a fresh machine

	h.recorder().Record(Invocation{})

	require.Len(t, h.sent, 1)
	assert.NotEmpty(t, h.lastEvent(t).DeviceID)
}

func TestRecorder_ReadsConfigOncePerProcess(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	loads := 0
	r := h.recorder()
	r.LoadConfig = func() (*config.Config, error) { loads++; return h.store.load() }

	require.True(t, r.Reporting())
	// A later opt-out applies from the next process.
	h.store.setState(State{DeviceID: "d", NoticeShown: true, Disabled: true})
	r.Record(Invocation{})

	assert.Equal(t, 1, loads)
	assert.Len(t, h.sent, 1)
	h.recorder().Record(Invocation{})
	assert.Len(t, h.sent, 1, "the next process sees the opt-out")
}

func TestRecord_OptOuts(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"VOLCANO_TELEMETRY_DISABLED": func(h *harness) { h.env[EnvDisabled] = "1" },
		"telemetry disable":          func(h *harness) { h.store.setState(State{Disabled: true}) },
		"unreadable config":          func(h *harness) { h.store.failLoad = true },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			setup(h)
			var stderr bytes.Buffer
			h.recorder().Record(Invocation{Stderr: &stderr})
			assert.Empty(t, h.sent)
			assert.Empty(t, stderr.String(), "an opt-out shows no notice")
			assert.Zero(t, h.store.saves)
		})
	}
}

func TestRecord_FeedbackNoticeWithTelemetryOff(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"VOLCANO_TELEMETRY_DISABLED": func(h *harness) { h.env[EnvDisabled] = "1" },
		"telemetry disable":          func(h *harness) { h.store.setState(State{Disabled: true}) },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.store.cfg.Feedback = nil
			setup(h)
			telemetryBefore := h.store.state()
			var stderr bytes.Buffer

			h.recorder().Record(Invocation{Stderr: &stderr})
			assert.Contains(t, stderr.String(), "volcano feedback --help", "first launch announces feedback")
			assert.NotContains(t, stderr.String(), "collects usage data", "telemetry is off")
			assert.True(t, h.store.cfg.Feedback.NoticeShown)
			assert.Equal(t, telemetryBefore, h.store.state(), "telemetry's record is unchanged")
			assert.Empty(t, h.sent)
			assert.True(t, h.recorder().FeedbackOn(), "feedback is on from the next process")

			stderr.Reset()
			h.recorder().Record(Invocation{Stderr: &stderr})
			assert.Empty(t, stderr.String(), "the notice is shown once")
		})
	}
}

func TestRecord_FeedbackNoticeAfterTelemetryNotice(t *testing.T) {
	// A machine that saw the notice before feedback existed.
	h := newHarness()
	h.store.cfg.Feedback = nil
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	var stderr bytes.Buffer

	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Contains(t, stderr.String(), "volcano feedback --help")
	assert.NotContains(t, stderr.String(), "Nothing is sent", "this run sends its usage event")
	assert.True(t, h.store.cfg.Feedback.NoticeShown)
	assert.Len(t, h.sent, 1, "telemetry was already announced, so this run still sends")

	stderr.Reset()
	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, stderr.String())
}

func TestRecord_BothOffShowsNoNotice(t *testing.T) {
	h := newHarness()
	h.store.cfg.Feedback = &config.FeedbackConfig{Disabled: true}
	h.env[EnvDisabled] = "1"
	var stderr bytes.Buffer

	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, stderr.String())
	assert.Zero(t, h.store.saves)
}

func TestRecord_FalsyEnvDoesNotOptOut(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.env[EnvDisabled] = "0"
	h.env["DO_NOT_TRACK"] = "1" // not an opt-out the CLI reads
	h.recorder().Record(Invocation{})
	assert.Len(t, h.sent, 1)
}

func TestRecord_SkipsCompletionScript(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	root := &cobra.Command{Use: "volcano"}
	docs := &cobra.Command{Use: "docs"}
	nested := &cobra.Command{Use: "completion"}
	docs.AddCommand(nested)
	root.AddCommand(docs)
	root.InitDefaultCompletionCmd()
	zsh, _, err := root.Find([]string{"completion", "zsh"})
	require.NoError(t, err)
	require.Equal(t, "volcano completion zsh", zsh.CommandPath())

	h.recorder().Record(Invocation{Command: zsh})
	assert.Empty(t, h.sent)

	h.recorder().Record(Invocation{Command: nested})
	assert.Len(t, h.sent, 1, "only the root's completion command is skipped")
}

func TestRecord_SkipAnnotation(t *testing.T) {
	h := newHarness()
	parent := &cobra.Command{Use: "telemetry", Annotations: map[string]string{SkipAnnotation: "true"}}
	cmd := &cobra.Command{Use: "disable"}
	parent.AddCommand(cmd)
	h.recorder().Record(Invocation{Command: cmd})
	assert.Empty(t, h.sent)
	assert.Zero(t, h.store.saves)
}

func TestRecord_DebugPrintsInsteadOfSending(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.env[EnvDebug] = "1"
	var stderr bytes.Buffer
	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, h.sent)
	assert.True(t, strings.HasPrefix(stderr.String(), "[telemetry] {"), stderr.String())
}

func TestRecord_EventFields(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "dev-1", NoticeShown: true})
	h.env["GITHUB_ACTIONS"] = "true"
	cmd := commandTree(t, "--all", "--name", "secret-value", "--debug")

	h.recorder().Record(Invocation{
		Command:  cmd,
		ExitCode: 0,
		Start:    h.now.Add(-1500 * time.Millisecond),
		StdinTTY: true,
	})

	e := h.lastEvent(t)
	assert.Equal(t, "dev-1", e.DeviceID)
	assert.NotEmpty(t, e.InvocationID)
	assert.Equal(t, "2026-10-15T02:59:58Z", e.Timestamp)
	assert.Equal(t, "cloud functions deploy", e.Command)
	assert.Equal(t, []string{"all", "debug", "name"}, e.Flags)
	assert.Equal(t, map[string]string{"name": "secret-value"}, e.Resources)
	assert.Equal(t, version.Version, e.CLIVersion)
	assert.Equal(t, runtime.GOOS, e.OS)
	assert.Equal(t, runtime.GOARCH, e.Arch)
	assert.Equal(t, runtime.NumCPU(), e.CPUs)
	assert.Equal(t, "github-actions", e.CIVendor)
	assert.True(t, e.StdinTTY)
	assert.Equal(t, int64(1500), e.DurationMS)
	assert.Empty(t, e.Agent)
	assert.Equal(t, []string{}, e.AgentMarkers)
	assert.False(t, e.PluginInstalled)
	assert.Empty(t, e.ErrorKind)
}

func TestRecord_ProjectAndResources(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "dev-1", NoticeShown: true})
	h.store.cfg.CurrentProject = &config.ProjectConfig{ID: "11111111-2222-3333-4444-555555555555", Name: "app"}

	h.recorder().Record(Invocation{Command: commandTree(t, "--name", "hello")})

	e := h.lastEvent(t)
	assert.Equal(t, "11111111-2222-3333-4444-555555555555", e.ProjectID)
	assert.Equal(t, map[string]string{"name": "hello"}, e.Resources)
}

func TestRecord_DropsPluginVersionThatIsNotAVersion(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.env["CLAUDECODE"] = "1"
	h.plugins["claude-code"] = "see /Users/alice/notes: hunter2"

	h.recorder().Record(Invocation{})

	e := h.lastEvent(t)
	assert.True(t, e.PluginInstalled)
	assert.Empty(t, e.PluginVersion)
}

func TestRecord_LocalCommandSendsNoCloudProject(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.store.cfg.CurrentProject = &config.ProjectConfig{ID: "11111111-2222-3333-4444-555555555555", Name: "app"}
	root := &cobra.Command{Use: "volcano"}
	functions := &cobra.Command{Use: "functions"}
	get := &cobra.Command{Use: "get <name-or-id>"}
	functions.AddCommand(get)
	root.AddCommand(functions)

	h.recorder().Record(Invocation{Command: get})

	assert.Empty(t, h.lastEvent(t).ProjectID)
}

func TestRecord_DropsErrorCodeFromSensitiveError(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	err := fmt.Errorf("invoke: %w", sensitive.Wrap(&api.Error{StatusCode: 400, Code: "private_customer_4411", Message: "failed"}))

	h.recorder().Record(Invocation{Err: err, ExitCode: 1})

	e := h.lastEvent(t)
	assert.Equal(t, 400, e.ErrorStatus)
	assert.Empty(t, e.ErrorCode)
	assert.Equal(t, "invoke: [redacted]", e.ErrorMessage)
}

func TestRecord_AgentRunReportsPluginAndError(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "dev-1", NoticeShown: true})
	h.env["CLAUDECODE"] = "1"
	h.plugins["claude-code"] = "0.2.16"
	apiErr := fmt.Errorf("deploy: %w", &api.Error{StatusCode: http.StatusForbidden, Code: "project_access_denied", Message: "no"})

	h.recorder().Record(Invocation{Err: apiErr, ExitCode: 1})

	e := h.lastEvent(t)
	assert.Equal(t, "claude-code", e.Agent)
	assert.Equal(t, "marker", e.AgentSource)
	assert.Equal(t, []string{"claude-code"}, e.AgentMarkers)
	assert.True(t, e.PluginInstalled)
	assert.Equal(t, "0.2.16", e.PluginVersion)
	assert.Equal(t, 1, e.ExitCode)
	assert.Equal(t, "api", e.ErrorKind)
	assert.Equal(t, http.StatusForbidden, e.ErrorStatus)
	assert.Equal(t, "project_access_denied", e.ErrorCode)
}

func TestRecord_HumanRunReportsError(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "dev-1", NoticeShown: true})
	h.plugins[""] = "" // a manual install must not be reported without an agent
	err := fmt.Errorf("read /home/dev/app/volcano.yaml with Bearer abc.def: %w", &api.Error{StatusCode: 500, Code: "internal", Message: "boom"})
	h.recorder().Record(Invocation{Err: err, ExitCode: 1})

	e := h.lastEvent(t)
	assert.Equal(t, 1, e.ExitCode)
	assert.Equal(t, "api", e.ErrorKind)
	assert.Equal(t, 500, e.ErrorStatus)
	assert.Equal(t, "internal", e.ErrorCode)
	assert.True(t, strings.HasPrefix(e.ErrorMessage, "read ~/app/volcano.yaml with Bearer [redacted] "), e.ErrorMessage)
	assert.False(t, e.PluginInstalled)
}

func TestRecord_SuccessSendsNoErrorFields(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "dev-1", NoticeShown: true})
	h.recorder().Record(Invocation{})
	assert.NotContains(t, string(h.sent[0]), "error_")
	assert.NotContains(t, string(h.sent[0]), "resources")
	assert.NotContains(t, string(h.sent[0]), "project_id")
}

func TestErrorKind(t *testing.T) {
	cases := []struct {
		want string
		err  error
	}{
		{"api", &api.Error{StatusCode: 404}},
		{"api", fmt.Errorf("deploy: %w", &api.ProjectConfigValidationError{})},
		{"local_not_running", fmt.Errorf("x: %w", cliruntime.ErrLocalNotRunning)},
		{"network", fmt.Errorf("cannot reach: %w", &net.OpError{Op: "dial", Err: errors.New("refused")})},
		{"network", fmt.Errorf("request: %w", &url.Error{Op: "Get", URL: "https://api.volcano.dev", Err: context.DeadlineExceeded})},
		{"other", fmt.Errorf("wait: %w", context.DeadlineExceeded)},
		{"other", fmt.Errorf("request: %w", &url.Error{Op: "Get", URL: "https://api.volcano.dev", Err: context.Canceled})},
		{"other", fmt.Errorf("api url: %w", &url.Error{Op: "parse", URL: "http://[::1", Err: errors.New("missing ']' in host")})},
		{"usage", errors.New(`unknown flag: --nope`)},
		{"usage", errors.New("at least one of the flags in the group [backup to] is required")},
		{"usage", errors.New("if any flags in the group [public private] are set none of the others can be; [private public] were all set")},
		{"usage", errors.New("if any flags in the group [cert key] are set they must all be set; missing [key]")},
		{"other", errors.New("docker failed")},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, errorKind(tc.err), tc.err.Error())
	}
}

func TestErrorKind_CobraFlagGroups(t *testing.T) {
	cases := map[string]func(*cobra.Command){
		"one required":       func(c *cobra.Command) { c.MarkFlagsOneRequired("a", "b") },
		"required together":  func(c *cobra.Command) { c.MarkFlagsRequiredTogether("a", "b") },
		"mutually exclusive": func(c *cobra.Command) { c.MarkFlagsMutuallyExclusive("a", "b") },
	}
	args := map[string][]string{
		"one required":       {"run"},
		"required together":  {"run", "--a", "x"},
		"mutually exclusive": {"run", "--a", "x", "--b", "y"},
	}
	for name, mark := range cases {
		t.Run(name, func(t *testing.T) {
			root := &cobra.Command{Use: "volcano", SilenceErrors: true, SilenceUsage: true}
			run := &cobra.Command{Use: "run", RunE: func(*cobra.Command, []string) error { return nil }}
			run.Flags().String("a", "", "")
			run.Flags().String("b", "", "")
			mark(run)
			root.AddCommand(run)
			root.SetArgs(args[name])
			err := root.Execute()
			require.Error(t, err)
			assert.Equal(t, "usage", errorKind(err), err.Error())
		})
	}
}

func TestHarnessFor(t *testing.T) {
	cases := map[string]string{
		"claude-code": "claude-code",
		"cowork":      "claude-code",
		"codex":       "codex",
		"cursor":      "cursor",
		"cursor-cli":  "cursor",
		"opencode":    "opencode",
		"pi":          "pi",
		"gemini-cli":  "",
		"windsurf":    "",
	}
	for in, want := range cases {
		assert.Equal(t, want, harnessFor(in), in)
	}
}

func TestCIVendor(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	assert.Empty(t, CIVendor(env(nil)))
	assert.Equal(t, "generic", CIVendor(env(map[string]string{"CI": "true"})))
	assert.Empty(t, CIVendor(env(map[string]string{"CI": "false"})))
	assert.Equal(t, "gitlab", CIVendor(env(map[string]string{"CI": "true", "GITLAB_CI": "true"})))
	assert.Equal(t, "buildkite", CIVendor(env(map[string]string{"BUILDKITE": "true"})))
}

func TestRecord_SavesStateInConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, config.Update(func(cfg *config.Config) (bool, error) {
		cfg.UserToken = "keep-me"
		return true, nil
	}))

	(&Recorder{Getenv: func(string) string { return "" }, Send: func([]byte) error { return nil }}).Record(Invocation{})

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "keep-me", cfg.UserToken)
	require.NotNil(t, cfg.Telemetry)
	assert.True(t, cfg.Telemetry.NoticeShown)
	assert.NotEmpty(t, cfg.Telemetry.DeviceID)
}

func TestFlush_PostsPayloadWithToken(t *testing.T) {
	var gotBody []byte
	var gotHeader http.Header
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Clone()
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_API_URL", srv.URL)
	t.Setenv("VOLCANO_TOKEN", "tok-123")

	require.NoError(t, Flush(t.Context(), strings.NewReader(`{"device_id":"d"}`), srv.Client()))

	assert.Equal(t, "/cli/telemetry", gotPath)
	assert.JSONEq(t, `{"device_id":"d"}`, string(gotBody))
	assert.Equal(t, "Bearer tok-123", gotHeader.Get("Authorization"))
	assert.Equal(t, "application/json", gotHeader.Get("Content-Type"))
	assert.True(t, strings.HasPrefix(gotHeader.Get("User-Agent"), "volcano-cli/"))
}

func TestFlush_NoTokenSendsNoAuthorization(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_API_URL", srv.URL)
	t.Setenv("VOLCANO_TOKEN", "")

	require.NoError(t, Flush(t.Context(), strings.NewReader(`{}`), srv.Client()))
	assert.Empty(t, gotAuth)
}

func TestFlush_RejectsOversizedAndEmptyPayloads(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.Error(t, Flush(t.Context(), strings.NewReader(""), nil))
	require.Error(t, Flush(t.Context(), strings.NewReader(strings.Repeat("a", maxPayloadBytes+1)), nil))
}

func TestFlush_ReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_API_URL", srv.URL)

	require.ErrorContains(t, Flush(t.Context(), strings.NewReader(`{}`), srv.Client()), "429")
}

func TestReporting(t *testing.T) {
	reporting := func(state State, setup func(h *harness)) bool {
		h := newHarness()
		h.store.setState(state)
		if setup != nil {
			setup(h)
		}
		return h.recorder().Reporting()
	}
	shown := State{DeviceID: "d", NoticeShown: true}
	assert.True(t, reporting(shown, nil))
	assert.False(t, reporting(State{}, nil), "nothing before the first-run notice")
	assert.False(t, reporting(shown, func(h *harness) { h.env[EnvDisabled] = "1" }))
	assert.False(t, reporting(State{DeviceID: "d", NoticeShown: true, Disabled: true}, nil))
	assert.False(t, reporting(shown, func(h *harness) { h.store.failLoad = true }), "an unreadable config counts as off")
}

func TestOfflineArg(t *testing.T) {
	cases := map[string]bool{
		"docs list --offline":                 true,
		"docs list --bogus --offline":         true,
		"docs list --offline=true":            true,
		"docs list --offline=false":           false,
		"docs list --offline=false --offline": true,
		"docs list --offline --offline=0":     false,
		"docs list":                           false,
		"functions invoke x -- --offline":     false,
		"docs list --offline-cache":           false,
	}
	for args, want := range cases {
		assert.Equal(t, want, offlineArg(strings.Fields(args)), args)
	}
}

func TestRecord_SkipsOfflineWhenFlagParsingFailed(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.recorder().Record(Invocation{Args: []string{"docs", "list", "--bogus", "--offline"}, Err: errors.New("unknown flag: --bogus"), ExitCode: 1})
	assert.Empty(t, h.sent)
}
