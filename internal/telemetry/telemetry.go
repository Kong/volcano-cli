// Package telemetry records one usage event per CLI invocation and sends it to
// the Volcano API, which forwards it to product analytics (volcano-hosting
// POST /cli/telemetry).
//
// What is sent is fixed by Event: command path, the names of flags the user
// set, the selected project and the resource names the command addressed (see
// resources), CLI version, platform, CI vendor, whether stdin is a terminal, the
// coding agent that ran the CLI, whether the Volcano plugin is installed for
// that agent, the exit code and duration, and for failed runs the error type,
// API status and code, and the redacted error message.
//
// Telemetry is on by default. VOLCANO_TELEMETRY_DISABLED or `volcano telemetry
// disable` turn it off. The settings live in the telemetry block of
// ~/.volcano/config.json, read once when the process starts: a change applies
// from the next command. The first recorded invocation prints a one-time
// notice. Sending happens in a detached `volcano telemetry flush` process so a
// command never waits on the network.
package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Kong/volcano-cli/internal/agent"
	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sensitive"
	"github.com/Kong/volcano-cli/internal/setup"
	"github.com/Kong/volcano-cli/internal/version"
)

// Environment variables that control telemetry.
const (
	// EnvDisabled turns telemetry off.
	EnvDisabled = "VOLCANO_TELEMETRY_DISABLED"
	// EnvFeedbackDisabled turns `volcano feedback` reports off, even after
	// `volcano feedback enable`.
	EnvFeedbackDisabled = "VOLCANO_FEEDBACK_DISABLED"
	// EnvDebug prints each event to stderr instead of sending it. Off unless
	// set.
	EnvDebug = "VOLCANO_TELEMETRY_DEBUG"
	// EnvFeedbackDebug appends each `volcano feedback` report to
	// ~/.volcano/feedback.jsonl instead of sending it. Nothing else changes,
	// including the output. For maintainers only, so it is not documented.
	EnvFeedbackDebug = "VOLCANO_FEEDBACK_DEBUG"
)

// SkipAnnotation marks a command whose invocations, and its subcommands', are
// never recorded (`volcano telemetry`, so an opt-out is not itself reported).
const SkipAnnotation = "telemetry.skip"

// State is the telemetry block of ~/.volcano/config.json.
type State = config.TelemetryConfig

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
	ErrorMessage    string   `json:"error_message,omitempty"`
	ProjectID       string   `json:"project_id,omitempty"`
	// Resources maps a usage-line placeholder or flag name to the resource
	// name or ID the command was given.
	Resources map[string]string `json:"resources,omitempty"`
}

// Invocation is what the entry point knows about the command that just ran.
type Invocation struct {
	Command  *cobra.Command // the executed command (cobra ExecuteC); may be nil
	Err      error
	ExitCode int
	Start    time.Time
	Stderr   io.Writer // receives the first-run notice and debug output
	StdinTTY bool
	// Args are the raw command-line arguments, checked for --offline when
	// flag parsing failed before cobra read it.
	Args []string
}

// Recorder records the invocation of one CLI process. It reads the config once,
// on first use, and answers both Reporting and Record from that read. The zero
// value uses the real environment, config, and flush process; tests replace the
// fields. A Recorder must not be copied after first use.
type Recorder struct {
	Getenv    func(string) string
	LookupEnv func(string) (string, bool)
	Now       func() time.Time
	// LoadConfig reads the CLI config (config.Load).
	LoadConfig func() (*config.Config, error)
	// UpdateConfig changes the config under its lock (config.Update).
	UpdateConfig func(mutate func(*config.Config) (bool, error)) error
	// HomeDir is replaced with "~" in error messages (os.UserHomeDir).
	HomeDir func() (string, error)
	Send    func(payload []byte) error
	// PluginStatus reports whether the Volcano plugin is installed for a
	// setup harness name; see setup.PluginStatus.
	PluginStatus func(harness string) (bool, string)

	once   sync.Once
	cfg    *config.Config
	cfgErr error

	// announceOnce guards announced, so the first-run notice is saved and
	// printed at most once per process, by AnnounceFeedback or Record.
	announceOnce sync.Once
	announced    announcement
}

// announcement is the result of the one attempt to save the first-run notice.
type announcement struct {
	state         State // the telemetry block after the attempt
	feedbackShown bool  // the saved config records the feedback notice
	err           error
}

// setup fills in the defaults and reads the config, once.
func (r *Recorder) setup() {
	r.once.Do(func() {
		if r.Getenv == nil {
			r.Getenv = os.Getenv
		}
		if r.LookupEnv == nil {
			r.LookupEnv = os.LookupEnv
		}
		if r.Now == nil {
			r.Now = time.Now
		}
		if r.LoadConfig == nil {
			r.LoadConfig = config.Load
		}
		if r.UpdateConfig == nil {
			r.UpdateConfig = config.Update
		}
		if r.HomeDir == nil {
			r.HomeDir = os.UserHomeDir
		}
		if r.Send == nil {
			r.Send = spawnFlush
		}
		if r.PluginStatus == nil {
			r.PluginStatus = func(harness string) (bool, string) {
				return setup.PluginStatus(setup.Options{}, harness)
			}
		}
		r.cfg, r.cfgErr = r.LoadConfig()
	})
}

// state is the telemetry block as the process found it at start.
func (r *Recorder) state() State {
	if r.cfg == nil || r.cfg.Telemetry == nil {
		return State{}
	}
	return *r.cfg.Telemetry
}

// DisabledByEnv reports whether VOLCANO_TELEMETRY_DISABLED turns telemetry off.
func DisabledByEnv(getenv func(string) string) bool {
	return isTruthy(getenv(EnvDisabled))
}

// FeedbackDisabledByEnv reports whether VOLCANO_FEEDBACK_DISABLED turns
// feedback off.
func FeedbackDisabledByEnv(getenv func(string) string) bool {
	return isTruthy(getenv(EnvFeedbackDisabled))
}

// FeedbackDebugByEnv reports whether VOLCANO_FEEDBACK_DEBUG saves reports
// locally instead of sending them.
func FeedbackDebugByEnv(getenv func(string) string) bool {
	return isTruthy(getenv(EnvFeedbackDebug))
}

// LoggedIn reports whether the config holds a login token that can send a
// feedback report. `volcano feedback` sends nothing without one. A project
// access token does not count: the server takes reports only from an account.
func (r *Recorder) LoggedIn() bool {
	r.setup()
	return r.cfg != nil && CanSendFeedback(r.cfg.Token())
}

// CanSendFeedback reports whether token can send a feedback report: it is set
// and is not a project access token.
func CanSendFeedback(token string) bool {
	return token != "" && !config.IsProjectToken(token)
}

// FeedbackOn reports whether `volcano feedback` may send a report: the config
// is readable, the feedback setting is on (see FeedbackSetting), and an
// earlier process showed the first-run notice for feedback.
func (r *Recorder) FeedbackOn() bool {
	r.setup()
	return r.cfgErr == nil && r.feedbackSettingOn() && r.feedbackNoticeShown()
}

// FeedbackState says whether the feedback setting is on, and if not, why.
type FeedbackState int

const (
	// FeedbackSettingOn means enabled with `volcano feedback enable`, or never
	// chosen while telemetry is on.
	FeedbackSettingOn FeedbackState = iota
	// FeedbackOffByEnv means VOLCANO_FEEDBACK_DISABLED is set.
	FeedbackOffByEnv
	// FeedbackOffByUser means turned off with `volcano feedback disable`.
	FeedbackOffByUser
	// FeedbackOffWithTelemetry means never chosen, and telemetry is off.
	FeedbackOffWithTelemetry
)

// FeedbackSetting returns the feedback setting for cfg, which may be nil for
// a home with no config. VOLCANO_FEEDBACK_DISABLED turns feedback off.
// Otherwise `volcano feedback enable` or `disable` decides, and when the user
// chose neither, feedback follows telemetry: a telemetry opt-out, in the
// config or by VOLCANO_TELEMETRY_DISABLED, also turns feedback off.
func FeedbackSetting(cfg *config.Config, getenv func(string) string) FeedbackState {
	if FeedbackDisabledByEnv(getenv) {
		return FeedbackOffByEnv
	}
	if cfg != nil && cfg.Feedback != nil && cfg.Feedback.Enabled != nil {
		if *cfg.Feedback.Enabled {
			return FeedbackSettingOn
		}
		return FeedbackOffByUser
	}
	if DisabledByEnv(getenv) || cfg != nil && cfg.Telemetry != nil && cfg.Telemetry.Disabled {
		return FeedbackOffWithTelemetry
	}
	return FeedbackSettingOn
}

// feedbackSettingOn reports whether the feedback setting is on.
func (r *Recorder) feedbackSettingOn() bool {
	return FeedbackSetting(r.cfg, r.Getenv) == FeedbackSettingOn
}

// feedbackNoticeShown reports whether the first-run notice was shown for
// feedback.
func (r *Recorder) feedbackNoticeShown() bool {
	return r.cfg != nil && r.cfg.Feedback != nil && r.cfg.Feedback.NoticeShown
}

// Reporting reports whether this process may send usage data, including the
// agent request headers: no env opt-out, not turned off with `volcano telemetry
// disable`, and the first-run notice already shown by an earlier process. A
// config that cannot be read, or a run before the notice (including every run
// in a home where the notice can never be saved), counts as off.
func (r *Recorder) Reporting() bool {
	r.setup()
	if DisabledByEnv(r.Getenv) || r.cfgErr != nil {
		return false
	}
	state := r.state()
	return !state.Disabled && state.NoticeShown
}

// Record records one invocation and shows the first-run notice. It never fails
// the command: every error is swallowed, since telemetry must not change what
// the CLI does.
//
// The notice covers usage data and feedback reports, and each has its own
// "shown" record. It is printed when either one is on and has not been
// announced yet. Feedback is off while telemetry is, unless the user ran
// `volcano feedback enable`, so a telemetry opt-out prints no notice and
// writes no config.
func (r *Recorder) Record(inv Invocation) {
	r.setup()
	if skipped(inv.Command, inv.Args) || r.cfgErr != nil {
		return
	}
	state := r.state()
	telemetryOn := r.telemetrySettingOn()
	startedBeforeNotice := !state.NoticeShown
	if r.needsNotice() {
		a := r.announce(inv.Stderr)
		switch {
		case a.err == nil:
			state = a.state
		// A home that cannot be saved, such as one mounted read-only, keeps
		// sending usage data when an earlier process already announced it.
		case startedBeforeNotice || state.DeviceID == "":
			return
		}
	}
	// A process that started before the notice was shown sends nothing,
	// even when a concurrent process showed it first: the user had no
	// chance to opt out before this command ran.
	if !telemetryOn || state.Disabled || startedBeforeNotice {
		return
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

// telemetrySettingOn reports whether usage data is on in the settings: no env
// opt-out and not turned off with `volcano telemetry disable`.
func (r *Recorder) telemetrySettingOn() bool {
	return !DisabledByEnv(r.Getenv) && !r.state().Disabled
}

// needsNotice reports whether the config the process started with lacks a
// first-run notice record for usage data or feedback while that one is on.
func (r *Recorder) needsNotice() bool {
	state := r.state()
	return r.telemetrySettingOn() && (state.DeviceID == "" || !state.NoticeShown) ||
		r.feedbackSettingOn() && !r.feedbackNoticeShown()
}

// announce saves the first-run notice for what needs it and prints it to w
// when the save succeeds. Only the first call does anything; later calls
// return its result, so Record after AnnounceFeedback prints nothing again.
func (r *Recorder) announce(w io.Writer) announcement {
	r.announceOnce.Do(func() {
		state, feedbackShown, announced, err := r.initialize(r.telemetrySettingOn(), r.feedbackSettingOn())
		r.announced = announcement{state: state, feedbackShown: feedbackShown, err: err}
		if err == nil && w != nil {
			printNotice(w, announced)
		}
	})
	return r.announced
}

// AnnounceFeedback shows the first-run notice now, when the feedback setting
// is on and feedback was not announced yet, and reports whether the config
// records the feedback notice, so a report from the next process is sent. It
// is false when the setting is off, the config cannot be read, or the notice
// cannot be saved, such as in a read-only home. `volcano feedback status`
// calls it so that its answer holds for the next command; Record in the same
// process then shows nothing again.
func (r *Recorder) AnnounceFeedback(w io.Writer) bool {
	r.setup()
	if r.cfgErr != nil || !r.feedbackSettingOn() {
		return false
	}
	if r.feedbackNoticeShown() {
		return true
	}
	a := r.announce(w)
	return a.err == nil && a.feedbackShown
}

// initialize marks the notice as shown for usage data when telemetry is on,
// giving the machine a device id, and for feedback when feedback is on. It
// runs under the config's lock and checks the saved settings again, so a
// concurrent `volcano telemetry disable` or `volcano feedback disable` is never
// overwritten. announced says which of the two this call marked, so the caller
// prints the notice for those, and feedbackShown whether the saved config
// records the feedback notice. When the config cannot be saved (a read-only
// home, a sandbox) it returns an error, so the caller prints no notice and
// sends nothing that was not announced before: printing it on every run would
// only add noise.
func (r *Recorder) initialize(telemetry, feedback bool) (state State, feedbackShown bool, announced notice, err error) {
	err = r.UpdateConfig(func(cfg *config.Config) (bool, error) {
		changed := false
		if telemetry && cfg.Telemetry == nil {
			cfg.Telemetry = &State{}
		}
		if s := cfg.Telemetry; telemetry && !s.Disabled {
			if s.DeviceID == "" {
				s.DeviceID = uuid.NewString()
				changed = true
			}
			if !s.NoticeShown {
				s.NoticeShown = true
				announced.telemetry = true
				changed = true
			}
		}
		if feedback && FeedbackSetting(cfg, r.Getenv) == FeedbackSettingOn && (cfg.Feedback == nil || !cfg.Feedback.NoticeShown) {
			if cfg.Feedback == nil {
				cfg.Feedback = &config.FeedbackConfig{}
			}
			cfg.Feedback.NoticeShown = true
			announced.feedback = true
			changed = true
		}
		if cfg.Telemetry != nil {
			state = *cfg.Telemetry
		}
		feedbackShown = cfg.Feedback != nil && cfg.Feedback.NoticeShown
		return changed, nil
	})
	return state, feedbackShown, announced, err
}

// skipped reports whether an invocation of cmd is never recorded: commands
// under a SkipAnnotation (`volcano telemetry`), shell completion (the script
// `volcano completion` prints, and the requests run on every Tab press with
// stderr discarded), and runs that promised not to touch the network
// (`--offline`).
func skipped(cmd *cobra.Command, args []string) bool {
	if offlineArg(args) {
		return true
	}
	if cmd == nil {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[SkipAnnotation] != "" {
			return true
		}
		// cobra's `volcano completion <shell>` prints the completion script,
		// often from a shell rc file on every new shell.
		if c.Name() == "completion" && c.HasParent() && !c.Parent().HasParent() {
			return true
		}
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

// offlineArg reports whether the raw arguments ask for --offline, so a run whose
// flag parsing failed before cobra read the flag still keeps its promise not to
// touch the network. Arguments after "--" are positional and do not count.
func offlineArg(args []string) bool {
	offline := false
	for _, arg := range args {
		if arg == "--" {
			break
		}
		// The last occurrence wins, as it does for cobra.
		if arg == "--offline" {
			offline = true
		} else if value, ok := strings.CutPrefix(arg, "--offline="); ok {
			offline = isTruthy(value)
		}
	}
	return offline
}

func (r *Recorder) build(inv Invocation, deviceID string) Event {
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
		Resources:    resources(inv.Command),
	}
	// The saved project is the cloud project; local commands run against the
	// local sandbox's project instead, so only cloud commands report it.
	if r.cfg != nil && strings.HasPrefix(event.Command+" ", "cloud ") {
		event.ProjectID = r.cfg.ProjectID()
	}
	if event.Flags == nil {
		event.Flags = []string{}
	}
	if event.AgentMarkers == nil {
		event.AgentMarkers = []string{}
	}
	if detected.Name != "" {
		event.PluginInstalled, event.PluginVersion = r.PluginStatus(harnessFor(detected.Name))
		// The version is read from a harness's own files; send it only when it
		// looks like a version.
		if !pluginVersionPattern.MatchString(event.PluginVersion) {
			event.PluginVersion = ""
		}
	}
	if inv.Err != nil {
		event.ErrorKind = errorKind(inv.Err)
		event.ErrorStatus = api.Status(inv.Err)
		// An error code inside a sensitive error came from the same untrusted
		// text, such as a function's own response body.
		if !sensitive.Contains(inv.Err, api.Code) {
			event.ErrorCode = api.Code(inv.Err)
		}
		home, _ := r.HomeDir()
		event.ErrorMessage = errorMessage(inv.Err, home)
	}
	return event
}

// pluginVersionPattern is the plugin version shape the API accepts.
var pluginVersionPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]{1,32})?$`)

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

// isNetworkError reports a failure to reach a server. context.DeadlineExceeded
// satisfies net.Error, but a command's own wait running out, as in a deploy
// wait or the login poll, is not one; an HTTP request that times out still
// counts, since the client wraps it in *url.Error. A cancelled command and a
// URL that does not parse are not network failures either.
func isNetworkError(err error) bool {
	if errors.Is(err, context.Canceled) {
		return false
	}
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return urlErr.Op != "parse"
	}
	_, ok := errors.AsType[net.Error](err)
	return ok && !errors.Is(err, context.DeadlineExceeded)
}

// isUsageError matches cobra's own argument and flag parsing errors, which are
// plain errors with fixed message prefixes.
func isUsageError(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag",
		"required flag", "invalid argument", "flag needs an argument",
		"accepts ", "requires at least", "requires at most", "requires exactly",
		// Flag groups (MarkFlagsRequiredTogether, MarkFlagsOneRequired,
		// MarkFlagsMutuallyExclusive).
		"if any flags in the group", "at least one of the flags in the group",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// notice says what the first-run notice announces: usage data, feedback
// reports, or both. Each has its own "shown" record, so a machine that saw one
// is told only about the other.
type notice struct {
	telemetry, feedback bool
}

// printNotice prints the first-run notice for what n announces. Nothing it
// announces is sent until the next command: a feedback report needs the
// notice shown by an earlier process (FeedbackOn), and the usage event of the
// run that shows the telemetry notice is not sent.
func printNotice(w io.Writer, n notice) {
	switch {
	case n.telemetry && n.feedback:
		fmt.Fprintln(w, "Note: the Volcano CLI collects usage data: the command and flag names you use, the project and resource names it acts on, error messages, CLI version, platform, and which coding agent ran it. It is linked to a random ID for this machine and, when you are logged in, to your Volcano account. This helps us improve the CLI. Coding agents can also send feedback reports about problems they hit with `volcano feedback`.")
		fmt.Fprintf(w, "Nothing is sent until your next command. Turn off usage data with `volcano telemetry disable` or %s=1, and feedback with `volcano feedback disable`. Details: `volcano telemetry --help` and `volcano feedback --help`.\n", EnvDisabled)
	case n.telemetry:
		fmt.Fprintln(w, "Note: the Volcano CLI collects usage data: the command and flag names you use, the project and resource names it acts on, error messages, CLI version, platform, and which coding agent ran it. It is linked to a random ID for this machine and, when you are logged in, to your Volcano account. This helps us improve the CLI.")
		fmt.Fprintf(w, "Nothing is sent until your next command. Turn it off with `volcano telemetry disable` or %s=1. Details: `volcano telemetry --help`.\n", EnvDisabled)
	case n.feedback:
		fmt.Fprintln(w, "Note: coding agents can send the Volcano team feedback reports about problems they hit with Volcano, with `volcano feedback`. Turn it off with `volcano feedback disable`. Details: `volcano feedback --help`.")
	}
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
