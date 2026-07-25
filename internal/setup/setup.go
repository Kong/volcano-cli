// Package setup installs Volcano agent skills/plugins into the coding-agent
// harnesses present on the machine. With no targeting flags it autodetects the
// installed harnesses, installs into each, and returns a per-harness report;
// when none are detected it falls back to a manual install under ~/.volcano.
package setup

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// Status is the outcome for one harness.
type Status string

const (
	// StatusInstalled means the harness was set up successfully.
	StatusInstalled Status = "installed"
	// StatusSkipped means the harness was not detected on this machine.
	StatusSkipped Status = "skipped"
	// StatusFailed means the harness was targeted but its install failed.
	StatusFailed Status = "failed"
	// StatusPlanned is a dry-run entry: what would be installed.
	StatusPlanned Status = "planned"
)

// Result is one harness's outcome.
type Result struct {
	Harness string
	Status  Status
	Detail  string
}

// Report is the full setup outcome.
type Report struct {
	Results []Result
	// Manual is true when the no-harness ~/.volcano fallback was used.
	Manual bool
}

// Failed reports whether any targeted harness failed to install.
func (r Report) Failed() bool {
	for _, res := range r.Results {
		if res.Status == StatusFailed {
			return true
		}
	}
	return false
}

// Options configures Run. Zero-value fields default to real implementations, so
// production callers can pass an empty Options; tests inject fakes.
type Options struct {
	HTTPDoer      HTTPDoer                     // skills fetch; default http.DefaultClient
	CommandRunner CommandRunner                // marketplace shell-out; default os/exec
	WebURL        string                       // default $VOLCANO_WEB_URL or https://volcano.dev
	HomeDir       string                       // default os.UserHomeDir()
	Getenv        func(string) string          // default os.Getenv
	LookPath      func(string) (string, error) // default exec.LookPath
	Only          []string                     // explicit --harness targets (bypasses autodetect)
	Manual        bool                         // force the ~/.volcano fallback
	DryRun        bool                         // report the plan without writing
}

// resolved is Options after defaulting, passed to per-harness install funcs.
type resolved struct {
	doer   HTTPDoer
	runner CommandRunner
	webURL string
}

const defaultWebURL = "https://volcano.dev"

// Run detects/targets harnesses and installs Volcano into each. It returns an
// error only for setup-wide problems (e.g. an unknown --harness); an individual
// harness failure is recorded in the report — inspect Report.Failed.
func Run(ctx context.Context, opts Options) (Report, error) {
	res, env, err := opts.resolve()
	if err != nil {
		return Report{}, err
	}
	all := harnesses()

	if len(opts.Only) > 0 {
		return runOnly(ctx, all, opts.Only, env, res, opts.DryRun)
	}
	if opts.Manual {
		return Report{Manual: true, Results: []Result{installManual(ctx, env, res, opts.DryRun)}}, nil
	}

	// Autodetect: record every harness, install the detected ones.
	var report Report
	detected := 0
	for _, h := range all {
		if !h.detect(env) {
			report.Results = append(report.Results, Result{Harness: h.name, Status: StatusSkipped, Detail: "not detected"})
			continue
		}
		detected++
		report.Results = append(report.Results, install(ctx, h, env, res, opts.DryRun))
	}
	if detected == 0 {
		return Report{Manual: true, Results: []Result{installManual(ctx, env, res, opts.DryRun)}}, nil
	}
	return report, nil
}

func runOnly(ctx context.Context, all []harness, only []string, env environ, res resolved, dryRun bool) (Report, error) {
	byName := make(map[string]harness, len(all))
	for _, h := range all {
		byName[h.name] = h
	}
	var report Report
	for _, raw := range only {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == manualHarness {
			report.Manual = true
			report.Results = append(report.Results, installManual(ctx, env, res, dryRun))
			continue
		}
		h, ok := byName[name]
		if !ok {
			return Report{}, fmt.Errorf("unknown harness %q (supported: %s)", raw, strings.Join(supportedNames(all), ", "))
		}
		report.Results = append(report.Results, install(ctx, h, env, res, dryRun))
	}
	return report, nil
}

func supportedNames(all []harness) []string {
	names := make([]string, 0, len(all)+1)
	for _, h := range all {
		names = append(names, h.name)
	}
	return append(names, manualHarness)
}

func install(ctx context.Context, h harness, env environ, res resolved, dryRun bool) Result {
	if dryRun {
		return Result{Harness: h.name, Status: StatusPlanned, Detail: "would install"}
	}
	detail, err := h.install(ctx, env, res)
	if err != nil {
		return Result{Harness: h.name, Status: StatusFailed, Detail: err.Error()}
	}
	return Result{Harness: h.name, Status: StatusInstalled, Detail: detail}
}

func installManual(ctx context.Context, env environ, res resolved, dryRun bool) Result {
	if dryRun {
		return Result{Harness: manualHarness, Status: StatusPlanned, Detail: "would install skills to ~/.volcano/skills"}
	}
	detail, err := manualInstall(ctx, env, res)
	if err != nil {
		return Result{Harness: manualHarness, Status: StatusFailed, Detail: err.Error()}
	}
	return Result{Harness: manualHarness, Status: StatusInstalled, Detail: detail}
}

func (o Options) resolve() (resolved, environ, error) {
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	home := o.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return resolved{}, environ{}, fmt.Errorf("cannot determine home directory: %w", err)
		}
		home = h
	}
	lookPath := o.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	doer := o.HTTPDoer
	if doer == nil {
		doer = http.DefaultClient
	}
	runner := o.CommandRunner
	if runner == nil {
		runner = execRunner{}
	}

	webURL := strings.TrimRight(strings.TrimSpace(o.WebURL), "/")
	if webURL == "" {
		if env := strings.TrimRight(strings.TrimSpace(getenv("VOLCANO_WEB_URL")), "/"); env != "" {
			webURL = env
		} else {
			webURL = defaultWebURL
		}
	}

	return resolved{doer: doer, runner: runner, webURL: webURL},
		environ{home: home, getenv: getenv, lookPath: lookPath},
		nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // fixed harness plugin commands
}

// RenderReport writes a human-readable per-harness summary to w.
func RenderReport(w io.Writer, r Report) {
	installed, failed, skipped := 0, 0, 0
	for _, res := range r.Results {
		switch res.Status {
		case StatusInstalled:
			installed++
		case StatusFailed:
			failed++
		case StatusSkipped:
			skipped++
		}
		line := fmt.Sprintf("  %-9s %-11s", statusMark(res.Status), res.Harness)
		if res.Detail != "" {
			line += " " + res.Detail
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w)
	switch {
	case r.Manual && failed > 0:
		fmt.Fprintln(w, "No coding-agent harness detected, and the manual install to ~/.volcano failed (see above).")
	case r.Manual:
		fmt.Fprintln(w, "No coding-agent harness detected — installed Volcano skills to ~/.volcano/skills.")
	default:
		fmt.Fprintf(w, "Installed Volcano for %d harness(es)", installed)
		if failed > 0 {
			fmt.Fprintf(w, "; %d failed", failed)
		}
		if skipped > 0 {
			fmt.Fprintf(w, "; %d not detected", skipped)
		}
		fmt.Fprintln(w, ".")
	}
}

func statusMark(s Status) string {
	switch s {
	case StatusInstalled:
		return "[ok]"
	case StatusFailed:
		return "[fail]"
	case StatusPlanned:
		return "[plan]"
	default:
		return "[skip]"
	}
}
