// Package setup installs Volcano agent skills/plugins into the coding-agent
// harnesses present on the machine. With no targeting flags it autodetects the
// installed harnesses, installs into each, and returns a per-harness report;
// when none are detected it falls back to a manual install under ~/.volcano.
package setup

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
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
	BestEffort    bool                         // downgrade Only-target install failures to detected (interactive default); --harness stays strict
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
		return runOnly(ctx, all, opts.Only, env, res, opts.DryRun, opts.BestEffort)
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
		report.Results = append(report.Results, bestEffortResult(install(ctx, h, env, res, opts.DryRun)))
	}
	if detected == 0 {
		return Report{ManualFallback: true, Results: []Result{installManual(ctx, env, res, opts.DryRun)}}, nil
	}
	return report, nil
}

// bestEffortResult downgrades a hard install failure to a detected-but-failed
// result, keeping the real reason on one line ("install failed: … returned
// status 500" rather than a bare failure). Used by the autodetect default and
// the interactive default selection so one harness failing — e.g. the skills
// endpoint not being live yet during rollout — doesn't fail the whole command.
// Explicit --harness targeting stays strict (BestEffort false) so a named
// target that fails is a hard error.
func bestEffortResult(r Result) Result {
	if r.Status != StatusFailed {
		return r
	}
	r.Status = StatusDetected
	r.Detail = "install failed: " + firstLine(r.Detail)
	return r
}

func runOnly(ctx context.Context, all []harness, only []string, env environ, res resolved, dryRun, bestEffort bool) (Report, error) {
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
		var r Result
		if t.manual {
			r = installManual(ctx, env, res, dryRun)
		} else {
			r = install(ctx, t.h, env, res, dryRun)
		}
		if bestEffort {
			r = bestEffortResult(r)
		}
		report.Results = append(report.Results, r)
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
	writeReport(w, r, colorEnabled(w))
}

// RenderReportString returns the same report as a string, colored when on is
// true regardless of the destination. RenderReport uses it for direct output;
// the interactive completion animation uses it to reveal the identical content
// line by line, so the animated finish mirrors the non-interactive report.
func RenderReportString(r Report, on bool) string {
	var b strings.Builder
	writeReport(&b, r, on)
	return b.String()
}

func writeReport(w io.Writer, r Report, on bool) {
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
		mark := styleMark(res.Status, fmt.Sprintf("%-11s", statusMark(res.Status)), on)
		line := fmt.Sprintf("  %s %-11s", mark, res.Harness)
		if res.Detail != "" {
			detail := res.Detail
			// On a failed or detected-but-failed row the detail is the error reason,
			// so show it in deep red.
			if res.Status == StatusFailed || res.Status == StatusDetected {
				detail = errText(detail, on)
			}
			line += " " + detail
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
		fmt.Fprintln(w, errText("No coding-agent harness detected, and the manual install to ~/.volcano failed (see above).", on))
	case r.ManualFallback:
		fmt.Fprintln(w, "No coding-agent harness detected — installed Volcano skills to ~/.volcano/skills.")
	case installed == 0 && detected == 0 && failed == 0:
		fmt.Fprintln(w, "No coding-agent harnesses were set up.")
	case installed == 0 && failed == 0:
		// Harnesses detected, but none installed (detected > 0 here).
		fmt.Fprintln(w, errText(fmt.Sprintf("Detected %d harness(es), but installation failed.", detected), on))
	default:
		fmt.Fprintf(w, "Installed Volcano for %d harness(es)", installed)
		if detected > 0 {
			fmt.Fprint(w, errText(fmt.Sprintf("; %d detected but failed to install", detected), on))
		}
		if failed > 0 {
			fmt.Fprint(w, errText(fmt.Sprintf("; %d failed", failed), on))
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

	// Once a directly-usable harness installed, nudge the user to try it — a
	// copy-paste prompt they can hand straight to their agent, picked at random to
	// show the range of apps Volcano backs. Skipped on dry runs and all-failed
	// reports, and on a manual-only install: its skills sit inert in ~/.volcano
	// until the user runs the import prompt printed above, so "You're set" would
	// contradict it.
	usable := false
	for _, res := range r.Results {
		if res.Status == StatusInstalled && res.Harness != manualHarness {
			usable = true
			break
		}
	}
	if usable {
		fmt.Fprintln(w)
		fmt.Fprintln(w, cta("You're set. Try asking your agent to build something:", on))
		fmt.Fprintln(w, "  "+cta(ctaExamples[rand.IntN(len(ctaExamples))], on)) //nolint:gosec // cosmetic CTA pick, not security-sensitive
	}
}

// ctaExamples are copy-paste starter prompts, one shown at random after a
// successful setup. Keep these to capabilities available directly through
// Volcano so every suggestion is a dependable first project.
var ctaExamples = []string{
	`"Build a personal notes app with email sign-in and per-user data using Volcano"`,
	`"Build a collaborative todo board with realtime updates using Volcano"`,
	`"Build a realtime chat app with online presence using Volcano"`,
	`"Build a live poll with realtime results using Volcano"`,
	`"Build a photo gallery with uploads and public sharing using Volcano Storage"`,
	`"Build a resumable file-sharing app using Volcano Storage"`,
	`"Build a URL shortener with click analytics using Volcano"`,
	`"Build a headless blog CMS with drafts and published posts using Volcano"`,
	`"Build a link-in-bio page with click tracking using Volcano"`,
	`"Build a feature-flag dashboard with per-user targeting using Volcano"`,
	`"Build a QR code generator using Volcano Functions"`,
	`"Build a live leaderboard using Volcano Realtime"`,
}

// firstLine reduces a possibly multi-line install error (e.g. a plugin
// command's combined output) to its first non-empty line so the one-line report
// rows stay aligned.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if before, _, found := strings.Cut(s, "\n"); found {
		return strings.TrimSpace(before)
	}
	return s
}

func statusMark(s Status) string {
	// Full words, consistent with the picker's [installed]/[available] marks:
	// [ok]/[fail]/[plan]/[skip] read as jargon next to those. [detected] already
	// reads clearly ("found, but the install didn't complete") and its Detail
	// column spells out the failure, so it stays.
	switch s {
	case StatusInstalled:
		return "[installed]"
	case StatusDetected:
		return "[detected]"
	case StatusFailed:
		return "[failed]"
	case StatusPlanned:
		return "[planned]"
	default:
		return "[skipped]"
	}
}
