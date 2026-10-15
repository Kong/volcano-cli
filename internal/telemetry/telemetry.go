// Package telemetry records one usage event per CLI invocation and sends it to
// the Volcano API, which forwards it to product analytics (volcano-hosting
// POST /cli/telemetry).
//
// What is sent is fixed by Event: command path, the names of flags the user
// set (never values or arguments), CLI version, platform, CI vendor, whether
// stdin is a terminal, the coding agent that ran the CLI, whether the Volcano
// plugin is installed for that agent, the exit code and duration, and for
// failed agent runs the API error status and code.
//
// Telemetry is on by default. VOLCANO_TELEMETRY_DISABLED, DO_NOT_TRACK, or
// `volcano telemetry disable` turn it off. The first recorded invocation prints
// a one-time notice. Sending happens in a detached `volcano telemetry flush`
// process so a command never waits on the network.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Kong/volcano-cli/internal/agent"
	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/setup"
	"github.com/Kong/volcano-cli/internal/version"
)

// Environment variables that control telemetry.
const (
	EnvDisabled   = "VOLCANO_TELEMETRY_DISABLED"
	EnvDoNotTrack = "DO_NOT_TRACK"
	EnvDebug      = "VOLCANO_TELEMETRY_DEBUG"
)

// SkipAnnotation marks a command whose invocations are never recorded (the
// hidden flush command itself).
const SkipAnnotation = "telemetry.skip"

// Event is the request body of POST /cli/telemetry. Field names are the wire
// contract with volcano-hosting; the server re-validates every value.
type Event struct {
	DeviceID        string   `json:"device_id"`
	InvocationID    string   `json:"invocation_id"`
	Timestamp       string   `json:"timestamp"`
	Command         string   `json:"command"`
	Flags           []string `json:"flags"`
	CLIVersion      string   `json:"cli_version"`
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	CPUs            int      `json:"cpus"`
	CIVendor        string   `json:"ci_vendor"`
	StdinTTY        bool     `json:"stdin_tty"`
	Agent           string   `json:"agent"`
	AgentSource     string   `json:"agent_source"`
	AgentMarkers    []string `json:"agent_markers"`
	PluginInstalled bool     `json:"plugin_installed"`
	PluginVersion   string   `json:"plugin_version"`
	ExitCode        int      `json:"exit_code"`
	DurationMS      int64    `json:"duration_ms"`
	ErrorKind       string   `json:"error_kind,omitempty"`
	ErrorStatus     int      `json:"error_status,omitempty"`
	ErrorCode       string   `json:"error_code,omitempty"`
}

// Invocation is what the entry point knows about the command that just ran.
type Invocation struct {
	Command  *cobra.Command // the executed command (cobra ExecuteC); may be nil
	Err      error
	ExitCode int
	Start    time.Time
	Stderr   io.Writer // receives the first-run notice and debug output
	StdinTTY bool
}

// Recorder records invocations. The zero value uses the real environment,
// state file, and flush process; tests replace the fields.
type Recorder struct {
	Getenv    func(string) string
	LookupEnv func(string) (string, bool)
	Now       func() time.Time
	State     StateStore
	Send      func(payload []byte) error
	// PluginStatus reports whether the Volcano plugin is installed for a
	// setup harness name; see setup.PluginStatus.
	PluginStatus func(harness string) (bool, string)
}

func (r Recorder) withDefaults() Recorder {
	if r.Getenv == nil {
		r.Getenv = os.Getenv
	}
	if r.LookupEnv == nil {
		r.LookupEnv = os.LookupEnv
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.State == nil {
		r.State = FileStateStore{}
	}
	if r.Send == nil {
		r.Send = spawnFlush
	}
	if r.PluginStatus == nil {
		r.PluginStatus = func(harness string) (bool, string) {
			return setup.PluginStatus(setup.Options{}, harness)
		}
	}
	return r
}

// DisabledByEnv returns the name of the environment variable that turns
// telemetry off, or "" when none does.
func DisabledByEnv(getenv func(string) string) string {
	for _, name := range []string{EnvDisabled, EnvDoNotTrack} {
		if isTruthy(getenv(name)) {
			return name
		}
	}
	return ""
}

// Enabled reports whether telemetry is on: no env opt-out and not turned off
// with `volcano telemetry disable`. A state file that cannot be read counts as
// off.
func Enabled(getenv func(string) string, store StateStore) bool {
	if DisabledByEnv(getenv) != "" {
		return false
	}
	state, err := store.Load()
	return err == nil && !state.Disabled
}

// Record records one invocation. It never fails the command: every error is
// swallowed, since telemetry must not change what the CLI does.
func (r Recorder) Record(inv Invocation) {
	r = r.withDefaults()
	if skipped(inv.Command) {
		return
	}
	// An env opt-out also means the user already knows: no notice.
	if DisabledByEnv(r.Getenv) != "" {
		return
	}
	state, err := r.State.Load()
	if err != nil || state.Disabled {
		return
	}
	if state.DeviceID == "" || !state.NoticeShown {
		var firstRun bool
		state, firstRun, err = r.initialize()
		if err != nil || state.Disabled {
			return
		}
		if firstRun {
			// The run that shows the notice sends nothing, so the user can
			// opt out before any event leaves the machine.
			if inv.Stderr != nil {
				printNotice(inv.Stderr)
			}
			return
		}
	}

	payload, err := json.Marshal(r.build(inv, state.DeviceID))
	if err != nil {
		return
	}
	if isTruthy(r.Getenv(EnvDebug)) {
		if inv.Stderr != nil {
			fmt.Fprintf(inv.Stderr, "[telemetry] %s\n", payload)
		}
		return
	}
	_ = r.Send(payload)
}

// initialize gives the machine a device id and marks the notice as shown, under
// the state file's lock so a concurrent `volcano telemetry disable` is never
// overwritten. firstRun reports whether this call marked the notice. When the
// state cannot be saved (a read-only home, a sandbox) it returns an error, so
// the caller neither prints the notice nor sends anything: printing it on every
// run would only add noise.
func (r Recorder) initialize() (state State, firstRun bool, err error) {
	err = r.State.Update(func(s *State) bool {
		if s.Disabled {
			state = *s
			return false
		}
		changed := false
		if s.DeviceID == "" {
			s.DeviceID = uuid.NewString()
			changed = true
		}
		if !s.NoticeShown {
			s.NoticeShown = true
			firstRun = true
			changed = true
		}
		state = *s
		return changed
	})
	return state, firstRun, err
}

// skipped reports whether an invocation of cmd is never recorded: the hidden
// flush command, shell completion requests (run on every Tab press with stderr
// discarded), and runs that promised not to touch the network (`--offline`).
func skipped(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Annotations[SkipAnnotation] != "" {
		return true
	}
	switch cmd.Name() {
	case cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	}
	if f := cmd.Flags().Lookup("offline"); f != nil && f.Changed && f.Value.String() == "true" {
		return true
	}
	return false
}

func (r Recorder) build(inv Invocation, deviceID string) Event {
	start := inv.Start
	if start.IsZero() {
		start = r.Now()
	}
	detected := agent.Detect(r.LookupEnv)
	event := Event{
		DeviceID:     deviceID,
		InvocationID: uuid.NewString(),
		Timestamp:    start.UTC().Format(time.RFC3339),
		Command:      commandPath(inv.Command),
		Flags:        flagsSet(inv.Command),
		CLIVersion:   version.Version,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		CPUs:         runtime.NumCPU(),
		CIVendor:     CIVendor(r.Getenv),
		StdinTTY:     inv.StdinTTY,
		Agent:        detected.Name,
		AgentSource:  string(detected.Source),
		AgentMarkers: detected.Markers,
		ExitCode:     inv.ExitCode,
		DurationMS:   r.Now().Sub(start).Milliseconds(),
	}
	if event.Flags == nil {
		event.Flags = []string{}
	}
	if event.AgentMarkers == nil {
		event.AgentMarkers = []string{}
	}
	if detected.Name != "" {
		event.PluginInstalled, event.PluginVersion = r.PluginStatus(harnessFor(detected.Name))
		if inv.Err != nil {
			event.ErrorKind = errorKind(inv.Err)
			event.ErrorStatus = api.Status(inv.Err)
			event.ErrorCode = api.Code(inv.Err)
		}
	}
	return event
}

// commandPath is the executed command's path without the root name, e.g.
// "cloud functions deploy"; "" for the root itself.
func commandPath(cmd *cobra.Command) string {
	if cmd == nil || !cmd.HasParent() {
		return ""
	}
	_, path, _ := strings.Cut(cmd.CommandPath(), " ")
	return path
}

// flagsSet lists the names of the flags the user set, sorted. Values are never
// read.
func flagsSet(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	var names []string
	cmd.Flags().Visit(func(f *pflag.Flag) { names = append(names, f.Name) })
	slices.Sort(names)
	return slices.Compact(names)
}

// harnessFor maps an agent name to the `volcano setup` harness that installs
// the Volcano plugin for it. An agent setup does not manage maps to "", which
// PluginStatus answers with the manual ~/.volcano install.
func harnessFor(agentName string) string {
	switch agentName {
	case "claude-code", "cowork":
		return "claude-code"
	case "codex":
		return "codex"
	case "cursor", "cursor-cli":
		return "cursor"
	case "opencode":
		return "opencode"
	case "pi":
		return "pi"
	}
	return ""
}

// errorKind sorts a failed command's error into a coarse category.
func errorKind(err error) string {
	switch {
	case api.Status(err) != 0:
		return "api"
	case errors.Is(err, cliruntime.ErrLocalNotRunning):
		return "local_not_running"
	case isNetworkError(err):
		return "network"
	case isUsageError(err):
		return "usage"
	}
	return "other"
}

func isNetworkError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

// isUsageError matches cobra's own argument and flag parsing errors, which are
// plain errors with fixed message prefixes.
func isUsageError(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag",
		"required flag", "invalid argument", "flag needs an argument",
		"accepts ", "requires at least", "requires at most", "requires exactly",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

func printNotice(w io.Writer) {
	fmt.Fprintln(w, "Note: the Volcano CLI collects usage data: the command and flag names you use (never their values), CLI version, platform, and which coding agent ran it. This helps us improve the CLI.")
	fmt.Fprintf(w, "Nothing is sent until your next command. Turn it off with `volcano telemetry disable` or %s=1. Details: `volcano docs get cli/telemetry.md`.\n", EnvDisabled)
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
