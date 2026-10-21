package telemetry

import (
	"runtime"
	"strings"
	"unicode"

	"github.com/Kong/volcano-cli/internal/agent"
	"github.com/Kong/volcano-cli/internal/version"
)

// Client describes this machine and the coding agent that ran the CLI, with the
// same values a usage event carries. `volcano feedback` attaches it to each
// report, so the server can join a report to the usage events of the same
// device. DeviceID and ProjectID are set only while telemetry is on.
type Client struct {
	DeviceID        string
	CLIVersion      string
	OS              string
	Arch            string
	Agent           string
	AgentSource     string
	PluginInstalled bool
	PluginVersion   string
	ProjectID       string
}

// Client returns what this process may report about the machine and the coding
// agent with a feedback report. The device ID and the project ID are set only
// while usage data may be sent (see Reporting), since either one ties a report
// to a person or a machine. A machine that has seen the notice but has no
// device ID yet, such as after `volcano telemetry enable` on a fresh home, gets
// one now.
func (r *Recorder) Client() Client {
	r.setup()
	detected := agent.Detect(r.LookupEnv)
	c := Client{
		CLIVersion:  version.Version,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Agent:       detected.Name,
		AgentSource: string(detected.Source),
	}
	if detected.Name != "" {
		c.PluginInstalled, c.PluginVersion = r.PluginStatus(harnessFor(detected.Name))
		if !pluginVersionPattern.MatchString(c.PluginVersion) {
			c.PluginVersion = ""
		}
	}
	if !r.Reporting() {
		return c
	}
	state := r.state()
	if state.DeviceID == "" {
		var err error
		if state, _, err = r.initialize(); err != nil || state.Disabled {
			return c
		}
	}
	c.DeviceID = state.DeviceID
	if r.cfg != nil {
		// Not ProjectID: it records a VOLCANO_PROJECT_ID use, and the CLI
		// would then warn that the command ran against that project.
		c.ProjectID = r.cfg.SelectedProjectID()
	}
	return c
}

// Home returns the home directory to pass to ScrubText, or "" when it is
// unknown.
func (r *Recorder) Home() string {
	r.setup()
	home, _ := r.HomeDir()
	return home
}

// ScrubText prepares free text, such as a feedback report, for sending: the
// home directory replaced with "~", terminal escapes removed, control
// characters and Unicode spaces other than newlines turned into a space,
// secrets redacted as in error messages, and surrounding whitespace trimmed.
// Unlike error messages, line breaks are kept. Spaces are normalized before
// redacting so a Unicode space cannot hide a secret from the patterns.
func ScrubText(s, home string) string {
	s = replaceHome(s, home)
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
	// Redact the whole text, not each line, so "Bearer" at the end of a line
	// still hides the token on the next one.
	lines := strings.Split(redact(s), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
