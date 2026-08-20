// Package runtime exposes process and timing dependencies for command tests.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	goruntime "runtime"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

// ErrLocalNotRunning signals that a command needs the local Volcano stack but it
// isn't up (the server container is absent or stopped). It is already
// user-actionable, so callers surface it verbatim instead of burying it under a
// generic wrapper like "failed to load config".
var ErrLocalNotRunning = errors.New("local development is not running; run 'volcano start' to launch it, or use 'volcano cloud <command>' to target your cloud project")

// Deps captures runtime dependencies that command and service tests replace.
type Deps struct {
	HTTPClient apiclient.HttpRequestDoer
	// APIBaseURL overrides the compiled cloud API URL for tests. Synthetic
	// local configs supply their API URL through ConfigLoader instead.
	APIBaseURL   string
	OpenBrowser  func(string) error
	NewTimer     func(time.Duration) Timer
	NewTicker    func(time.Duration) Ticker
	ConfigLoader func() (*config.Config, error)
	// LocalMode makes session-built API clients send no credential. Local mode
	// is a single-tenant sandbox; the local server defaults an absent credential
	// to the pre-provisioned local user. Set by the local command wiring.
	LocalMode           bool
	LocalCommandRunner  CommandRunner
	UpdateCommandRunner CommandRunner
	GitCommandRunner    CommandRunner
	// GitTerminalRunner runs the git commands that write, which need the user's
	// terminal rather than a captured buffer: a push can prompt for credentials
	// and reports progress as it goes.
	GitTerminalRunner TerminalCommandRunner
	ExecutablePath    string
	UpdateGitHubAPIURL  string
	CommandPathPrefix   string
	// DocsCacheDir overrides the base cache directory used by `volcano docs`.
	// Empty uses os.UserCacheDir()/volcano. Tests inject a t.TempDir().
	DocsCacheDir string
	// DocsGitHubAPIURL overrides the GitHub API base (default
	// https://api.github.com) for docs sync. Tests point this at an httptest
	// server.
	DocsGitHubAPIURL string
	// DocsRawBaseURL selects raw-host downloads (e.g.
	// https://raw.githubusercontent.com) for public sources. It is empty by
	// default: downloads then go through the authenticated GitHub contents API
	// (which also works for private repos). Tests may point it at an httptest
	// server to exercise the raw-host path.
	DocsRawBaseURL string
	// Now overrides the wall clock for deterministic freshness/staleness tests.
	Now func() time.Time
}

// CommandPath returns a user-facing command path for the current command tree.
func CommandPath(deps Deps, command string) string {
	prefix := deps.CommandPathPrefix
	if prefix == "" {
		prefix = "volcano"
	}
	if command == "" {
		return prefix
	}
	return prefix + " " + command
}

// CommandRunner runs an external command.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// CommandRunnerFunc adapts a function to CommandRunner.
type CommandRunnerFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Run calls f(ctx, name, args...).
func (f CommandRunnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

// TerminalCommandRunner runs an external command with the user's terminal
// attached: output reaches out as the command produces it, and the command can
// prompt on stdin. CommandRunner captures both instead, which is right for
// reading git's answers and wrong for a push — a credential prompt with no
// terminal fails, and a captured progress stream leaves the user watching
// nothing during an upload.
type TerminalCommandRunner interface {
	Run(ctx context.Context, out io.Writer, name string, args ...string) error
}

// TerminalCommandRunnerFunc adapts a function to TerminalCommandRunner.
type TerminalCommandRunnerFunc func(ctx context.Context, out io.Writer, name string, args ...string) error

// Run calls f(ctx, out, name, args...).
func (f TerminalCommandRunnerFunc) Run(ctx context.Context, out io.Writer, name string, args ...string) error {
	return f(ctx, out, name, args...)
}

// Timer is the subset of time.Timer used by command services.
type Timer interface {
	C() <-chan time.Time
	Stop()
}

// Ticker is the subset of time.Ticker used by command services.
type Ticker interface {
	C() <-chan time.Time
	Reset(time.Duration)
	Stop()
}

type realTimer struct {
	timer *time.Timer
}

func (t realTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t realTimer) Stop() {
	t.timer.Stop()
}

type realTicker struct {
	ticker *time.Ticker
}

func (t realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t realTicker) Reset(duration time.Duration) {
	t.ticker.Reset(duration)
}

func (t realTicker) Stop() {
	t.ticker.Stop()
}

// NewTimer returns the configured test timer or a real time.Timer wrapper.
func NewTimer(deps Deps, duration time.Duration) Timer {
	if deps.NewTimer != nil {
		return deps.NewTimer(duration)
	}
	return realTimer{timer: time.NewTimer(duration)}
}

// NewTicker returns the configured test ticker or a real time.Ticker wrapper.
func NewTicker(deps Deps, duration time.Duration) Ticker {
	if deps.NewTicker != nil {
		return deps.NewTicker(duration)
	}
	return realTicker{ticker: time.NewTicker(duration)}
}

// validateOpenURL returns an error unless rawURL is an http(s) URL safe to hand
// to the platform browser launcher. Kept separate from OpenURL so the guard is
// testable without shelling out to open/xdg-open/rundll32.
func validateOpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open non-http(s) url: %q", rawURL)
	}
	return nil
}

// OpenURL opens rawURL in the platform default browser. It refuses any
// non-http(s) URL so a malicious or misconfigured backend can't make the CLI
// hand file://, custom-scheme, or flag-like values to open/xdg-open/rundll32.
func OpenURL(rawURL string) error {
	if err := validateOpenURL(rawURL); err != nil {
		return err
	}
	// gosec G204: the command name is a constant per platform; only the URL arg varies.
	ctx := context.Background()
	switch goruntime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "open", rawURL).Start() //nolint:gosec // constant cmd
	case "windows":
		return exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL).Start() //nolint:gosec // constant cmd
	default:
		return exec.CommandContext(ctx, "xdg-open", rawURL).Start() //nolint:gosec // constant cmd
	}
}

// OpenBrowser opens rawURL through the configured browser hook or OpenURL.
func OpenBrowser(deps Deps, rawURL string) error {
	if deps.OpenBrowser != nil {
		return deps.OpenBrowser(rawURL)
	}
	return OpenURL(rawURL)
}
