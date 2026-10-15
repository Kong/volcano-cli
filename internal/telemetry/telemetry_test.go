package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/version"
)

type memStore struct {
	state    State
	saves    int
	failSave bool
}

func (m *memStore) Load() (State, error) { return m.state, nil }
func (m *memStore) Update(fn func(*State) bool) error {
	if m.failSave {
		return errors.New("read-only home")
	}
	if fn(&m.state) {
		m.saves++
	}
	return nil
}

type harness struct {
	env     map[string]string
	store   *memStore
	sent    [][]byte
	plugins map[string]string // harness -> version; present means installed
	now     time.Time
}

func newHarness() *harness {
	return &harness{
		env:     map[string]string{},
		store:   &memStore{},
		plugins: map[string]string{},
		now:     time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC),
	}
}

func (h *harness) recorder() Recorder {
	return Recorder{
		Getenv: func(k string) string { return h.env[k] },
		LookupEnv: func(k string) (string, bool) {
			v, ok := h.env[k]
			return v, ok
		},
		Now:   func() time.Time { return h.now },
		State: h.store,
		Send:  func(p []byte) error { h.sent = append(h.sent, p); return nil },
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
	assert.Contains(t, stderr.String(), "volcano docs get cli/telemetry.md")
	assert.Empty(t, h.sent, "the run that shows the notice sends nothing")
	assert.True(t, h.store.state.NoticeShown)
	deviceID := h.store.state.DeviceID
	assert.NotEmpty(t, deviceID)

	stderr.Reset()
	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, stderr.String())
	require.Len(t, h.sent, 1)
	assert.Equal(t, deviceID, h.lastEvent(t).DeviceID)
	assert.Equal(t, 1, h.store.saves)
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
	h.store.state = State{DeviceID: "d", NoticeShown: true}

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

// staleStore returns the state as it was before a concurrent
// `volcano telemetry disable`, while Update sees the file as it is now.
type staleStore struct{ FileStateStore }

func (staleStore) Load() (State, error) { return State{}, nil }

func TestRecord_FirstRunKeepsConcurrentOptOut(t *testing.T) {
	file := FileStateStore{Path: filepath.Join(t.TempDir(), "telemetry.json")}
	require.NoError(t, file.Update(func(s *State) bool { s.Disabled = true; return true }))
	var stderr bytes.Buffer
	r := Recorder{State: staleStore{file}, Getenv: func(string) string { return "" }, Send: func([]byte) error {
		t.Fatal("must not send after opt-out")
		return nil
	}}

	r.Record(Invocation{Stderr: &stderr})

	got, err := file.Load()
	require.NoError(t, err)
	assert.Equal(t, State{Disabled: true}, got, "the first-run write must not clobber the opt-out")
	assert.Empty(t, stderr.String())
}

func TestRecord_OptOuts(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"VOLCANO_TELEMETRY_DISABLED": func(h *harness) { h.env[EnvDisabled] = "1" },
		"DO_NOT_TRACK":               func(h *harness) { h.env[EnvDoNotTrack] = "true" },
		"telemetry disable":          func(h *harness) { h.store.state = State{Disabled: true} },
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

func TestRecord_FalsyEnvDoesNotOptOut(t *testing.T) {
	h := newHarness()
	h.store.state = State{DeviceID: "d", NoticeShown: true}
	h.env[EnvDisabled] = "0"
	h.env[EnvDoNotTrack] = "false"
	h.recorder().Record(Invocation{})
	assert.Len(t, h.sent, 1)
}

func TestRecord_SkipAnnotation(t *testing.T) {
	h := newHarness()
	cmd := &cobra.Command{Use: "flush", Annotations: map[string]string{SkipAnnotation: "true"}}
	h.recorder().Record(Invocation{Command: cmd})
	assert.Empty(t, h.sent)
	assert.Zero(t, h.store.saves)
}

func TestRecord_DebugPrintsInsteadOfSending(t *testing.T) {
	h := newHarness()
	h.store.state = State{DeviceID: "d", NoticeShown: true}
	h.env[EnvDebug] = "1"
	var stderr bytes.Buffer
	h.recorder().Record(Invocation{Stderr: &stderr})
	assert.Empty(t, h.sent)
	assert.True(t, strings.HasPrefix(stderr.String(), "[telemetry] {"), stderr.String())
}

func TestRecord_EventFields(t *testing.T) {
	h := newHarness()
	h.store.state = State{DeviceID: "dev-1", NoticeShown: true}
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
	assert.NotContains(t, string(h.sent[0]), "secret-value", "flag values are never sent")
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

func TestRecord_AgentRunReportsPluginAndError(t *testing.T) {
	h := newHarness()
	h.store.state = State{DeviceID: "dev-1", NoticeShown: true}
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

func TestRecord_HumanRunSendsNoErrorDetails(t *testing.T) {
	h := newHarness()
	h.store.state = State{DeviceID: "dev-1", NoticeShown: true}
	h.plugins[""] = "" // a manual install must not be reported without an agent
	h.recorder().Record(Invocation{Err: &api.Error{StatusCode: 500, Code: "internal"}, ExitCode: 1})

	e := h.lastEvent(t)
	assert.Equal(t, 1, e.ExitCode)
	assert.Empty(t, e.ErrorKind)
	assert.Zero(t, e.ErrorStatus)
	assert.Empty(t, e.ErrorCode)
	assert.False(t, e.PluginInstalled)
	assert.NotContains(t, string(h.sent[0]), "error_kind")
}

func TestErrorKind(t *testing.T) {
	cases := map[string]error{
		"api":               &api.Error{StatusCode: 404},
		"local_not_running": fmt.Errorf("x: %w", cliruntime.ErrLocalNotRunning),
		"network":           fmt.Errorf("cannot reach: %w", &net.OpError{Op: "dial", Err: errors.New("refused")}),
		"usage":             errors.New(`unknown flag: --nope`),
		"other":             errors.New("docker failed"),
	}
	for want, err := range cases {
		assert.Equal(t, want, errorKind(err), err.Error())
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

func TestFileStateStore_RoundTrip(t *testing.T) {
	store := FileStateStore{Path: filepath.Join(t.TempDir(), "nested", "telemetry.json")}
	got, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, State{}, got)

	want := State{DeviceID: "abc", Disabled: true, NoticeShown: true}
	require.NoError(t, store.Update(func(s *State) bool { *s = want; return true }))
	got, err = store.Load()
	require.NoError(t, err)
	assert.Equal(t, want, got)
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

func TestEnabled(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	assert.True(t, Enabled(env(nil), &memStore{}))
	assert.False(t, Enabled(env(map[string]string{EnvDoNotTrack: "1"}), &memStore{}))
	assert.False(t, Enabled(env(nil), &memStore{state: State{Disabled: true}}))
	assert.False(t, Enabled(env(nil), FileStateStore{Path: t.TempDir()}), "an unreadable state file counts as off")
}
