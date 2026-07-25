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
	"time"
)

// Status is the outcome for one harness.
type Status string

const (
	// StatusInstalled means the harness was set up successfully.
	StatusInstalled Status = "installed"
	// StatusDetected means the harness was found on the machine but its install
	// didn't complete (autodetect best-effort, not a command failure).
	StatusDetected Status = "detected"
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
	// ManualFallback is true only when no harness was detected and setup fell
	// back to ~/.volcano. An explicit --manual (or --harness manual) produces a
	// "manual" result but leaves this false: it was requested, not a fallback.
	ManualFallback bool
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
	SkillsBaseURL string                       // skills source; default the volcano-skills GitHub raw base (tests inject a mock)
	HomeDir       string                       // default os.UserHomeDir()
	Getenv        func(string) string          // default os.Getenv
	LookPath      func(string) (string, error) // default exec.LookPath
	Only          []string                     // explicit --harness targets (bypasses autodetect)
	Manual        bool                         // force the ~/.volcano fallback
	DryRun        bool                         // report the plan without writing
}

// resolved is Options after defaulting, passed to per-harness install funcs.
type resolved struct {
	doer       HTTPDoer
	runner     CommandRunner
	skillsBase string
}

const (
	// defaultSkillsBase is the canonical skills source: the Kong/volcano-skills
	// GitHub repo (also vendored into volcano-agentic-plugins as a submodule),
	// served as raw file content. Setup reads skills only from here — never from
	// volcano.dev, which is the web-app origin, not a skills host.
	defaultSkillsBase = "https://raw.githubusercontent.com/Kong/volcano-skills/main"
	// httpTimeout bounds each skill/manifest download when the caller does not
	// inject its own client.
	httpTimeout = 30 * time.Second
)

// Run detects/targets harnesses and installs Volcano into each. It returns an
// error only for setup-wide problems (e.g. an unknown --harness). Autodetect is
// best-effort: a detected harness whose install fails is reported as detected
// (not installed) rather than failed. Only an explicit --harness target records
// a failure — inspect Report.Failed.
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
		return Report{Results: []Result{installManual(ctx, env, res, opts.DryRun)}}, nil
	}

	// Autodetect is best-effort. Undetected harnesses are recorded as skipped and
	// omitted from the rendered report. A detected harness we couldn't finish
	// setting up — e.g. the skills endpoint isn't live yet — is reported as
	// detected (install failed), not a hard failure; only an explicit --harness
	// target turns an install failure into a command error.
	var report Report
	detected := 0
	for _, h := range all {
		if !h.detect(env) {
			report.Results = append(report.Results, Result{Harness: h.name, Status: StatusSkipped, Detail: "not detected"})
			continue
		}
		detected++
		r := install(ctx, h, env, res, opts.DryRun)
		if r.Status == StatusFailed {
			// Detected, but its install didn't complete: say so plainly instead of
			// failing the whole command. The raw error is dropped — it's plumbing like
			// "fetch skills index returned HTML" that means nothing to a user; rerun
			// with --harness <name> to see it.
			r.Status, r.Detail = StatusDetected, "install failed"
		}
		report.Results = append(report.Results, r)
	}
	if detected == 0 {
		return Report{ManualFallback: true, Results: []Result{installManual(ctx, env, res, opts.DryRun)}}, nil
	}
	return report, nil
}

func runOnly(ctx context.Context, all []harness, only []string, env environ, res resolved, dryRun bool) (Report, error) {
	byName := make(map[string]harness, len(all))
	for _, h := range all {
		byName[h.name] = h
	}

	// Resolve every requested name first, so an unknown target fails before any
	// install mutates the machine (no partial setup followed by an error).
	type target struct {
		manual bool
		h      harness
	}
	targets := make([]target, 0, len(only))
	for _, raw := range only {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == manualHarness {
			targets = append(targets, target{manual: true})
			continue
		}
		h, ok := byName[name]
		if !ok {
			return Report{}, fmt.Errorf("unknown harness %q (supported: %s)", raw, strings.Join(supportedNames(all), ", "))
		}
		targets = append(targets, target{h: h})
	}

	var report Report
	for _, t := range targets {
		if t.manual {
			report.Results = append(report.Results, installManual(ctx, env, res, dryRun))
			continue
		}
		report.Results = append(report.Results, install(ctx, t.h, env, res, dryRun))
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
		// A bounded client, not http.DefaultClient: production wires an empty
		// Deps{} and cobra runs on context.Background(), so without a timeout a
		// stalled origin would hang setup indefinitely.
		doer = &http.Client{Timeout: httpTimeout}
	}
	runner := o.CommandRunner
	if runner == nil {
		runner = execRunner{}
	}

	skillsBase := strings.TrimRight(strings.TrimSpace(o.SkillsBaseURL), "/")
	if skillsBase == "" {
		skillsBase = defaultSkillsBase
	}

	return resolved{doer: doer, runner: runner, skillsBase: skillsBase},
		environ{home: home, getenv: getenv, lookPath: lookPath},
		nil
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // fixed harness plugin commands
}

// RenderReport writes a human-readable per-harness summary to w. The footer is
// derived from the actual result statuses, so a dry run says "would install"
// and only a genuine no-detection fallback claims none was detected.
func RenderReport(w io.Writer, r Report) {
	installed, detected, failed, planned := 0, 0, 0, 0
	for _, res := range r.Results {
		switch res.Status {
		case StatusInstalled:
			installed++
		case StatusDetected:
			detected++
		case StatusFailed:
			failed++
		case StatusSkipped:
			// Only negative detection is hidden: undetected harnesses aren't listed,
			// in both real and dry runs. Everything detected is shown.
			continue
		case StatusPlanned:
			planned++
		}
		line := fmt.Sprintf("  %-10s %-11s", statusMark(res.Status), res.Harness)
		if res.Detail != "" {
			line += " " + res.Detail
		}
		fmt.Fprintln(w, line)
	}

	fmt.Fprintln(w)
	switch {
	case planned > 0 && r.ManualFallback:
		fmt.Fprintln(w, "No coding-agent harness detected — would install Volcano skills to ~/.volcano/skills.")
	case planned > 0:
		fmt.Fprintf(w, "Would install Volcano for %d harness(es).\n", planned)
	case r.ManualFallback && failed > 0:
		fmt.Fprintln(w, "No coding-agent harness detected, and the manual install to ~/.volcano failed (see above).")
	case r.ManualFallback:
		fmt.Fprintln(w, "No coding-agent harness detected — installed Volcano skills to ~/.volcano/skills.")
	case installed == 0 && detected == 0 && failed == 0:
		fmt.Fprintln(w, "No coding-agent harnesses were set up.")
	case installed == 0 && failed == 0:
		// Harnesses detected, but none installed (detected > 0 here).
		fmt.Fprintf(w, "Detected %d harness(es), but installation failed.\n", detected)
	default:
		fmt.Fprintf(w, "Installed Volcano for %d harness(es)", installed)
		if detected > 0 {
			fmt.Fprintf(w, "; %d detected but failed to install", detected)
		}
		if failed > 0 {
			fmt.Fprintf(w, "; %d failed", failed)
		}
		fmt.Fprintln(w, ".")
	}

	// A manual install drops skills into ~/.volcano, which no agent reads on its
	// own. Rather than auto-editing config, hand the user a prompt to relay to
	// whatever agent they use so it wires the store into its own rules/skills.
	for _, res := range r.Results {
		if res.Harness == manualHarness && res.Status == StatusInstalled {
			fmt.Fprintln(w, "To finish, ask your coding agent:")
			fmt.Fprintln(w, `  "Import @~/.volcano/AGENTS.md into your global rules and install the skills from @~/.volcano/skills."`)
			break
		}
	}
}

func statusMark(s Status) string {
	switch s {
	case StatusInstalled:
		return "[ok]"
	case StatusDetected:
		return "[detected]"
	case StatusFailed:
		return "[fail]"
	case StatusPlanned:
		return "[plan]"
	default:
		return "[skip]"
	}
}
