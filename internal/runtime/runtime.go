// Package runtime exposes process and timing dependencies for command tests.
package runtime

import (
	"context"
	"os/exec"
	goruntime "runtime"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
)

// Deps captures runtime dependencies that command and service tests replace.
type Deps struct {
	HTTPClient apiclient.HttpRequestDoer
	// APIBaseURL overrides the compiled cloud API URL for tests. Synthetic
	// local configs supply their API URL through ConfigLoader instead.
	APIBaseURL          string
	OpenBrowser         func(string) error
	NewTimer            func(time.Duration) Timer
	NewTicker           func(time.Duration) Ticker
	ConfigLoader        func() (*config.Config, error)
	LocalCommandRunner  CommandRunner
	UpdateCommandRunner CommandRunner
	ExecutablePath      string
	UpdateGitHubAPIURL  string
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

// OpenURL opens rawURL in the platform default browser.
func OpenURL(rawURL string) error {
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
