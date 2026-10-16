package telemetry

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/version"
)

func TestClient(t *testing.T) {
	h := newHarness()
	h.store.setState(State{DeviceID: "d", NoticeShown: true})
	h.env["CLAUDECODE"] = "1"
	h.plugins["claude-code"] = "0.2.16"

	c, ok := h.recorder().Client()
	require.True(t, ok)
	assert.Equal(t, Client{
		DeviceID:        "d",
		CLIVersion:      version.Version,
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		Agent:           "claude-code",
		AgentSource:     "marker",
		PluginInstalled: true,
		PluginVersion:   "0.2.16",
	}, c)
}

func TestClient_NotReporting(t *testing.T) {
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
			_, ok := h.recorder().Client()
			assert.False(t, ok)
		})
	}
}

func TestClient_MissingDeviceIDGetsOne(t *testing.T) {
	h := newHarness()
	h.store.setState(State{NoticeShown: true})

	c, ok := h.recorder().Client()
	require.True(t, ok)
	assert.NotEmpty(t, c.DeviceID)
	assert.Equal(t, h.store.state().DeviceID, c.DeviceID)
}

func TestScrubText(t *testing.T) {
	in := "ran \x1b[31mvolcano deploy\x1b[0m in /home/dev/app\r\n" +
		"error: Authorization: Bearer abc.def\n" +
		"DB_PASSWORD=hunter2 token=x\n\n" +
		"  kept   spacing  \n"
	want := "ran volcano deploy in ~/app\n" +
		"error: Authorization: Bearer [redacted]\n" +
		"DB_PASSWORD=[redacted] token=[redacted]\n\n" +
		"  kept   spacing"
	assert.Equal(t, want, ScrubText(in, "/home/dev"))
}
