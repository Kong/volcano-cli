package telemetry

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/telemetry"
)

type memStore struct{ state telemetry.State }

func (m *memStore) Load() (telemetry.State, error) { return m.state, nil }
func (m *memStore) Save(s telemetry.State) error   { m.state = s; return nil }

func runCmd(t *testing.T, store telemetry.StateStore, args ...string) (string, string) {
	t.Helper()
	cmd := New(store)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.String(), stderr.String()
}

func TestTelemetryCommand(t *testing.T) {
	t.Setenv(telemetry.EnvDisabled, "")
	t.Setenv(telemetry.EnvDoNotTrack, "")
	store := &memStore{state: telemetry.State{DeviceID: "d"}}

	out, _ := runCmd(t, store)
	assert.Contains(t, out, "Telemetry is enabled.")

	out, _ = runCmd(t, store, "disable")
	assert.Equal(t, "Telemetry is disabled.\n", out)
	assert.Equal(t, telemetry.State{DeviceID: "d", Disabled: true, NoticeShown: true}, store.state)

	out, _ = runCmd(t, store, "status")
	assert.Contains(t, out, "Telemetry is disabled.")

	out, _ = runCmd(t, store, "enable")
	assert.Equal(t, "Telemetry is enabled.\n", out)
	assert.False(t, store.state.Disabled)
}

func TestTelemetryCommand_EnvOptOut(t *testing.T) {
	t.Setenv(telemetry.EnvDisabled, "")
	t.Setenv(telemetry.EnvDoNotTrack, "1")
	store := &memStore{}

	out, _ := runCmd(t, store, "status")
	assert.Equal(t, "Telemetry is disabled by DO_NOT_TRACK.\n", out)

	_, stderr := runCmd(t, store, "enable")
	assert.Contains(t, stderr, "DO_NOT_TRACK is set")
}

func TestFlushIsHiddenAndNeverRecorded(t *testing.T) {
	flush, _, err := New(&memStore{}).Find([]string{"flush"})
	require.NoError(t, err)
	assert.True(t, flush.Hidden)
	assert.NotEmpty(t, flush.Annotations[telemetry.SkipAnnotation])
}
