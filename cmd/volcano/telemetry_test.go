package main

import (
	"errors"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

// TestMain keeps every test in this package from writing the telemetry state
// file or starting a flush process.
func TestMain(m *testing.M) {
	recordTelemetry = func(telemetry.Invocation) {}
	os.Exit(m.Run())
}

func captureTelemetry(t *testing.T) *[]telemetry.Invocation {
	t.Helper()
	var got []telemetry.Invocation
	old := recordTelemetry
	recordTelemetry = func(inv telemetry.Invocation) { got = append(got, inv) }
	t.Cleanup(func() { recordTelemetry = old })
	return &got
}

func TestRunRecordsExecutedCommandAndExitCode(t *testing.T) {
	resetInstructions(t)
	got := captureTelemetry(t)
	root := &cobra.Command{Use: "volcano", SilenceErrors: true, SilenceUsage: true}
	deploy := &cobra.Command{Use: "deploy", RunE: func(*cobra.Command, []string) error {
		return errors.New("boom")
	}}
	deploy.Flags().Bool("all", false, "")
	root.AddCommand(deploy)
	root.SetArgs([]string{"deploy", "--all"})
	root.SetErr(new(nopWriter))

	require.Equal(t, 1, run(root, cliruntime.Deps{}))

	require.Len(t, *got, 1)
	inv := (*got)[0]
	assert.Equal(t, deploy, inv.Command)
	assert.Equal(t, 1, inv.ExitCode)
	require.EqualError(t, inv.Err, "boom")
	assert.False(t, inv.Start.IsZero())
}

func TestRunRecordsSuccess(t *testing.T) {
	resetInstructions(t)
	got := captureTelemetry(t)
	root := &cobra.Command{Use: "volcano", RunE: func(*cobra.Command, []string) error { return nil }}
	root.SetArgs(nil)

	require.Equal(t, 0, run(root, cliruntime.Deps{}))

	require.Len(t, *got, 1)
	assert.Equal(t, 0, (*got)[0].ExitCode)
	assert.NoError(t, (*got)[0].Err)
}

type nopWriter struct{}

func (*nopWriter) Write(p []byte) (int, error) { return len(p), nil }
