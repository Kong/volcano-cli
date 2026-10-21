package telemetry

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/version"
)

func TestClient(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.store.cfg.CurrentProject = &config.ProjectConfig{ID: "p"}
	h.env["CLAUDECODE"] = "1"
	h.plugins["claude-code"] = "0.2.16"

	assert.Equal(t, Client{
		DeviceID:        "d",
		CLIVersion:      version.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Agent:           "claude-code",
		AgentSource:     "marker",
		PluginInstalled: true,
		PluginVersion:   "0.2.16",
		ProjectID:       "p",
	}, h.recorder().Client())
}

func TestClient_NotReportingHasNoIDs(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"before the notice": func(*harness) {},
		"disabled":          func(h *harness) { h.store.setState(State{DeviceID: "d", NoticeShown: true, Disabled: true}) },
		"env opt-out": func(h *harness) {
			h.store.setState(State{DeviceID: "d", NoticeShown: true})
			h.env[EnvDisabled] = "1"
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			setup(h)
			h.store.cfg.CurrentProject = &config.ProjectConfig{ID: "p"}
			h.env["CLAUDECODE"] = "1"

			c := h.recorder().Client()
			assert.Empty(t, c.DeviceID)
			assert.Empty(t, c.ProjectID)
			assert.Equal(t, "claude-code", c.Agent)
			assert.Equal(t, version.Version, c.CLIVersion)
		})
	}
}

func TestClient_MissingDeviceIDGetsOne(t *testing.T) {
	h := newHarness()
	h.store.setState(State{NoticeShown: true})

	c := h.recorder().Client()
	assert.NotEmpty(t, c.DeviceID)
	assert.Equal(t, h.store.state().DeviceID, c.DeviceID)
}

func TestFeedbackOn(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(h *harness)
		want  bool
	}{
		"on":                {func(h *harness) { h.store.setState(State{NoticeShown: true}) }, true},
		"telemetry off":     {func(h *harness) { h.store.setState(State{NoticeShown: true, Disabled: true}) }, true},
		"before the notice": {func(*harness) {}, false},
		"disabled": {func(h *harness) {
			h.store.setState(State{NoticeShown: true})
			h.store.cfg.Feedback = &config.FeedbackConfig{Disabled: true}
		}, false},
		"env opt-out": {func(h *harness) {
			h.store.setState(State{NoticeShown: true})
			h.env[EnvFeedbackDisabled] = "1"
		}, false},
		"unreadable config": {func(h *harness) {
			h.store.setState(State{NoticeShown: true})
			h.store.failLoad = true
		}, false},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			tc.setup(h)
			assert.Equal(t, tc.want, h.recorder().FeedbackOn())
		})
	}
}

func TestScrubText(t *testing.T) {
	in := "ran \x1b[31mvolcano deploy\x1b[0m in /home/dev/app\r\n" +
		"error: Authorization: Bearer abc.def\n" +
		"mail dev@example.com, see https://api.example.com/x?key=abc Bearer\u00a0xyz\n\n" +
		"  kept   spacing  \n"
	want := "ran volcano deploy in ~/app\n" +
		"error: Authorization: Bearer [redacted]\n" +
		"mail [redacted], see https://api.example.com/x?[redacted] Bearer [redacted]\n\n" +
		"  kept   spacing"
	assert.Equal(t, want, ScrubText(in, "/home/dev"))
}

func TestScrubText_BearerAcrossLines(t *testing.T) {
	assert.Equal(t, "Authorization: Bearer [redacted]\nnext", ScrubText("Authorization: Bearer\nabc.def\nnext", ""))
}

func TestScrubText_QuotedWindowsHome(t *testing.T) {
	in := `Error: open "C:\\Users\\alice\\app\\cert.pem": not found` + "\n" + `C:\Users\alice\app`
	want := `Error: open "~\\app\\cert.pem": not found` + "\n" + `~\app`
	assert.Equal(t, want, ScrubText(in, `C:\Users\alice`))
}
