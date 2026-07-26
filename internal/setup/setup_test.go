package setup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillsServer serves a minimal skills manifest + content, mirroring the
// Kong/volcano-skills GitHub raw layout the CLI fetches from: /index.json,
// /<name>/SKILL.md, /AGENTS.md.
func skillsServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"skills":[
			{"name":"volcano-platform"},
			{"name":"install-volcano"}]}`)
	})
	mux.HandleFunc("/AGENTS.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "# Volcano AGENTS.md\n")
	})
	// Everything else is a skill file: /<name>/SKILL.md.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "# Skill "+r.URL.Path+"\nVolcano skill content\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func noBins(string) (string, error) { return "", errors.New("not found") }

func emptyEnv(string) string { return "" }

type runResult struct {
	out []byte
	err error
}

type fakeRunner struct {
	calls   [][]string
	err     error       // fallback error for every call when results is unset/exhausted
	results []runResult // scripted per-call results, consumed in order
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if len(f.results) > 0 {
		r := f.results[0]
		f.results = f.results[1:]
		return r.out, r.err
	}
	return nil, f.err
}

// absentStatus is what statusOf returns when a harness is not in the report.
const absentStatus = Status("<absent>")

func statusOf(r Report, harness string) Status {
	for _, res := range r.Results {
		if res.Harness == harness {
			return res.Status
		}
	}
	return absentStatus
}

func TestRun_AutodetectInstallsDetected(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor"))
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))
	mustMkdir(t, filepath.Join(home, ".config", "opencode"))
	srv := skillsServer(t)

	report, err := Run(context.Background(), Options{
		HTTPDoer:      srv.Client(),
		SkillsBaseURL: srv.URL,
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      noBins, // no claude/codex on PATH
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() {
		t.Fatalf("unexpected failure: %+v", report.Results)
	}
	for _, h := range []string{"cursor", "opencode", "pi"} {
		if got := statusOf(report, h); got != StatusInstalled {
			t.Errorf("%s: status = %q, want installed", h, got)
		}
	}
	for _, h := range []string{"claude-code", "codex"} {
		if got := statusOf(report, h); got != StatusSkipped {
			t.Errorf("%s: status = %q, want skipped", h, got)
		}
	}
	// A dropped skill and (for opencode) AGENTS.md must exist on disk.
	assertFile(t, filepath.Join(home, ".cursor", "skills", "volcano-platform", "SKILL.md"), "Volcano skill content")
	assertFile(t, filepath.Join(home, ".pi", "agent", "skills", "install-volcano", "SKILL.md"), "Volcano skill content")
	assertFile(t, filepath.Join(home, ".config", "opencode", "skills", "volcano-platform", "SKILL.md"), "Volcano skill content")
	// opencode's global AGENTS.md is user-owned and must never be written.
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("opencode AGENTS.md must not be written (user-owned)")
	}
}

// Autodetect is best-effort and hides only negative detection: a detected
// harness whose install fails (here, a skills endpoint that errors) is shown as
// [detected], not [failed], so `volcano setup` surfaces the detection without
// exiting non-zero.
func TestRun_AutodetectShowsDetectedInstallFailures(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor"))
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))
	badSkills := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badSkills.Close)

	report, err := Run(context.Background(), Options{
		HTTPDoer:      badSkills.Client(),
		SkillsBaseURL: badSkills.URL,
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      noBins,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() {
		t.Fatalf("autodetect must not fail the command: %+v", report.Results)
	}
	// A detected harness whose install failed is shown as detected, not installed
	// or failed.
	for _, h := range []string{"cursor", "pi"} {
		if got := statusOf(report, h); got != StatusDetected {
			t.Errorf("%s: status = %q, want detected", h, got)
		}
	}
	var b strings.Builder
	RenderReport(&b, report)
	if strings.Contains(b.String(), "[failed]") {
		t.Errorf("autodetect report must not show [failed]:\n%s", b.String())
	}
	// Each detected-but-failed row must plainly say it failed to install AND keep
	// the real reason (here the origin's 500) rather than a bare "install failed".
	if !strings.Contains(b.String(), "[detected]") || !strings.Contains(b.String(), "install failed") {
		t.Errorf("want [detected] rows saying install failed, got:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "status 500") {
		t.Errorf("detected-but-failed row should surface the real reason (status 500), got:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "Detected 2 harness(es), but installation failed.") {
		t.Errorf("footer should note detected harnesses failed to install:\n%s", b.String())
	}
}

// TestRun_LandingPaths is the definitive check that each file-drop harness (and
// the manual fallback) materializes the FULL skill set into its exact expected
// directory, plus AGENTS.md where that harness expects one. Uses --harness to
// target each in isolation.
func TestRun_LandingPaths(t *testing.T) {
	// Skills the test manifest advertises (skillsServer).
	skills := []string{"volcano-platform", "install-volcano"}

	cases := []struct {
		harness    string
		skillsDir  func(home string) string
		agentsPath func(home string) string // nil = harness expects no AGENTS.md
	}{
		{
			harness:   "cursor",
			skillsDir: func(h string) string { return filepath.Join(h, ".cursor", "skills") },
		},
		{
			harness:   "opencode",
			skillsDir: func(h string) string { return filepath.Join(h, ".config", "opencode", "skills") },
			// user-owned AGENTS.md is intentionally not written.
		},
		{
			harness:   "pi",
			skillsDir: func(h string) string { return filepath.Join(h, ".pi", "agent", "skills") },
		},
		{
			harness:    "manual",
			skillsDir:  func(h string) string { return filepath.Join(h, ".volcano", "skills") },
			agentsPath: func(h string) string { return filepath.Join(h, ".volcano", "AGENTS.md") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.harness, func(t *testing.T) {
			home := t.TempDir()
			srv := skillsServer(t)
			report, err := Run(context.Background(), Options{
				HTTPDoer:      srv.Client(),
				SkillsBaseURL: srv.URL,
				HomeDir:       home,
				Getenv:        emptyEnv,
				LookPath:      noBins,
				Only:          []string{tc.harness},
			})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if report.Failed() {
				t.Fatalf("install failed: %+v", report.Results)
			}

			// Every advertised skill lands as <skillsDir>/<name>/SKILL.md.
			dir := tc.skillsDir(home)
			for _, name := range skills {
				assertFile(t, filepath.Join(dir, name, "SKILL.md"), "Volcano skill content")
			}
			// No stray skill dirs beyond the manifest.
			if entries, err := os.ReadDir(dir); err == nil && len(entries) != len(skills) {
				t.Errorf("%s: %d skill dirs in %s, want %d", tc.harness, len(entries), dir, len(skills))
			}

			// AGENTS.md lands only where the harness expects it.
			if tc.agentsPath != nil {
				assertFile(t, tc.agentsPath(home), "Volcano AGENTS.md")
			}
		})
	}
}

func TestRun_MarketplaceHarnessShellsOut(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{
		CommandRunner: runner,
		SkillsBaseURL: "http://example.invalid",
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath: func(bin string) (string, error) { // only claude present
			if bin == "claude" {
				return "/usr/bin/claude", nil
			}
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := statusOf(report, "claude-code"); got != StatusInstalled {
		t.Fatalf("claude-code status = %q, want installed", got)
	}
	wantClaude := []string{
		"claude plugin marketplace add " + marketplaceRepo,
		"claude plugin marketplace update " + marketplaceName,
		"claude plugin install " + pluginRef,
		"claude plugin update " + pluginRef,
	}
	assertCalls(t, runner.calls, wantClaude)
}

// Codex uses a different verb (`plugin add`) and pins the marketplace ref.
func TestRun_CodexUsesPluginAdd(t *testing.T) {
	runner := &fakeRunner{}
	_, err := Run(context.Background(), Options{
		CommandRunner: runner,
		SkillsBaseURL: "http://example.invalid",
		HomeDir:       t.TempDir(),
		Getenv:        emptyEnv,
		LookPath: func(bin string) (string, error) {
			if bin == "codex" {
				return "/usr/bin/codex", nil
			}
			return "", errors.New("not found")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertCalls(t, runner.calls, []string{
		"codex plugin marketplace add " + marketplaceRepo + " --ref main",
		"codex plugin marketplace upgrade " + marketplaceName,
		"codex plugin add " + pluginRef,
	})
}

func assertCalls(t *testing.T, got [][]string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("runner calls = %d, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if joined := strings.Join(got[i], " "); joined != w {
			t.Errorf("call %d = %q, want %q", i, joined, w)
		}
	}
}

func onlyCodex(bin string) (string, error) {
	if bin == "codex" {
		return "/usr/bin/codex", nil
	}
	return "", errors.New("not found")
}

// A rerun of `volcano setup`: the marketplace add reports our marketplace is
// "already added" (non-zero exit), then the plugin add succeeds. Both commands
// must run and the harness must end installed, not spuriously failed.
func TestRun_MarketplaceRerunToleratesAlreadyPresent(t *testing.T) {
	runner := &fakeRunner{results: []runResult{
		{out: []byte("error: marketplace 'volcano-agentic-plugins' already added from this source"), err: errors.New("exit status 1")},
		{}, // codex plugin marketplace upgrade succeeds
		{}, // codex plugin add succeeds
	}}
	report, err := Run(context.Background(), Options{
		CommandRunner: runner,
		HomeDir:       t.TempDir(),
		Getenv:        emptyEnv,
		LookPath:      onlyCodex,
		Only:          []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Failed() {
		t.Fatalf("rerun with 'already added' output must not fail: %+v", report.Results)
	}
	if got := statusOf(report, "codex"); got != StatusInstalled {
		t.Fatalf("codex status = %q, want installed on idempotent rerun", got)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("all codex commands must run, got %v", runner.calls)
	}
}

// A genuine failure on the terminal command (no following step to catch it)
// must stay fatal even when its output contains a generic "already …" phrase,
// as long as it doesn't name our plugin — e.g. a filesystem error.
func TestRun_MarketplaceTerminalFailureStaysFatal(t *testing.T) {
	runner := &fakeRunner{results: []runResult{
		{}, // marketplace add succeeds
		{}, // marketplace upgrade succeeds
		{out: []byte("mkdir /opt/plugins: destination directory already exists"), err: errors.New("exit status 1")}, // terminal plugin add
	}}
	report, err := Run(context.Background(), Options{
		CommandRunner: runner,
		HomeDir:       t.TempDir(),
		Getenv:        emptyEnv,
		LookPath:      onlyCodex,
		Only:          []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("unrelated terminal failure must stay fatal: %+v", report.Results)
	}
}

func TestRun_NoHarnessFallsBackToManual(t *testing.T) {
	home := t.TempDir()
	srv := skillsServer(t)
	report, err := Run(context.Background(), Options{
		HTTPDoer:      srv.Client(),
		SkillsBaseURL: srv.URL,
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      noBins,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.ManualFallback {
		t.Fatalf("expected manual fallback, got %+v", report.Results)
	}
	if got := statusOf(report, "manual"); got != StatusInstalled {
		t.Fatalf("manual status = %q, want installed", got)
	}
	assertFile(t, filepath.Join(home, ".volcano", "skills", "volcano-platform", "SKILL.md"), "Volcano skill content")
	assertFile(t, filepath.Join(home, ".volcano", "AGENTS.md"), "Volcano AGENTS.md")
}

func TestRun_ManualFlagForcesManual(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor")) // present but must be ignored
	srv := skillsServer(t)
	report, err := Run(context.Background(), Options{
		HTTPDoer:      srv.Client(),
		SkillsBaseURL: srv.URL,
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      noBins,
		Manual:        true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Results) != 1 || statusOf(report, "manual") != StatusInstalled {
		t.Fatalf("expected single installed manual result, got %+v", report.Results)
	}
	// An explicit --manual is a request, not a no-detection fallback.
	if report.ManualFallback {
		t.Errorf("--manual must not be reported as a fallback")
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "skills")); !os.IsNotExist(err) {
		t.Errorf("cursor skills should not be written under --manual")
	}
}

func TestRun_OnlyUnknownHarnessErrors(t *testing.T) {
	_, err := Run(context.Background(), Options{
		HomeDir:  t.TempDir(),
		Getenv:   emptyEnv,
		LookPath: noBins,
		Only:     []string{"eclipse"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown harness") {
		t.Fatalf("want unknown harness error, got %v", err)
	}
}

func TestRun_DryRunWritesNothing(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor"))
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{
		// A nil HTTPDoer would default to the real client; a dry run must never
		// reach it. Inject one that fails the test if called.
		HTTPDoer: doerFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("dry-run must not fetch")
			return nil, errors.New("unreachable")
		}),
		CommandRunner: runner,
		SkillsBaseURL: "http://example.invalid",
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      func(bin string) (string, error) { return "/usr/bin/" + bin, nil }, // claude+codex "present"
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, res := range report.Results {
		if res.Status != StatusPlanned && res.Status != StatusSkipped {
			t.Errorf("%s: status = %q, want planned/skipped in dry-run", res.Harness, res.Status)
		}
	}
	if len(runner.calls) != 0 {
		t.Errorf("dry-run must not shell out, got %v", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "skills")); !os.IsNotExist(err) {
		t.Errorf("dry-run must not write skills")
	}
}

func TestRun_FailedHarnessMarksReportFailed(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{err: errors.New("boom")}
	report, err := Run(context.Background(), Options{
		CommandRunner: runner,
		HomeDir:       home,
		Getenv:        emptyEnv,
		LookPath:      func(bin string) (string, error) { return "/usr/bin/" + bin, nil },
		Only:          []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("expected Report.Failed() = true, got %+v", report.Results)
	}
}

// A landed manual install must hand the user a relay prompt (the store in
// ~/.volcano is inert until an agent is pointed at it); a dry run or a
// non-manual report must not.
func TestRenderReport_ManualPickupGuidance(t *testing.T) {
	const prompt = "ask your coding agent"

	var b strings.Builder
	RenderReport(&b, Report{ManualFallback: true, Results: []Result{{Harness: "manual", Status: StatusInstalled, Detail: "11 skills -> ~/.volcano/skills"}}})
	if !strings.Contains(b.String(), prompt) || !strings.Contains(b.String(), "@~/.volcano/AGENTS.md") {
		t.Errorf("manual install should print pickup guidance:\n%s", b.String())
	}

	b.Reset()
	RenderReport(&b, Report{Results: []Result{{Harness: "claude-code", Status: StatusInstalled, Detail: "marketplace"}}})
	if strings.Contains(b.String(), prompt) {
		t.Errorf("non-manual report must not print pickup guidance:\n%s", b.String())
	}

	b.Reset()
	RenderReport(&b, Report{ManualFallback: true, Results: []Result{{Harness: "manual", Status: StatusPlanned}}})
	if strings.Contains(b.String(), prompt) {
		t.Errorf("dry-run must not print pickup guidance:\n%s", b.String())
	}
}

// A successful install ends with a randomly chosen copy-paste CTA; a dry run or
// an all-failed report must not print one (nothing is wired up to build against).
func TestRenderReport_BuildCTA(t *testing.T) {
	containsExample := func(s string) bool {
		for _, ex := range ctaExamples {
			if strings.Contains(s, ex) {
				return true
			}
		}
		return false
	}

	// Whichever index the pick lands on, the rendered output must contain a known
	// example. Looped so a regression that indexed out of range or off the list
	// would surface across draws, not just one.
	for range 50 {
		var b strings.Builder
		RenderReport(&b, Report{Results: []Result{{Harness: "claude-code", Status: StatusInstalled}}})
		if !containsExample(b.String()) {
			t.Fatalf("installed report should print one of the CTA examples:\n%s", b.String())
		}
	}

	var b strings.Builder
	RenderReport(&b, Report{Results: []Result{{Harness: "cursor", Status: StatusPlanned}}})
	if containsExample(b.String()) {
		t.Errorf("dry-run must not print a build CTA:\n%s", b.String())
	}

	b.Reset()
	RenderReport(&b, Report{Results: []Result{{Harness: "cursor", Status: StatusDetected}}})
	if containsExample(b.String()) {
		t.Errorf("detected-only report must not print a build CTA:\n%s", b.String())
	}

	b.Reset()
	RenderReport(&b, Report{Results: []Result{{Harness: "codex", Status: StatusFailed}}})
	if containsExample(b.String()) {
		t.Errorf("all-failed report must not print a build CTA:\n%s", b.String())
	}

	// A manual-only install leaves skills inert in ~/.volcano until the user runs
	// the "To finish" import prompt, so the CTA must stay quiet even though manual
	// counts as installed.
	b.Reset()
	RenderReport(&b, Report{ManualFallback: true, Results: []Result{{Harness: "manual", Status: StatusInstalled}}})
	if containsExample(b.String()) {
		t.Errorf("manual-only install must not print a build CTA:\n%s", b.String())
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

// firstLine must reduce a multi-line install error (e.g. a plugin command's
// combined output) to its first non-empty line so report rows stay aligned.
func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"single":           "single",
		"first\nsecond":    "first",
		"  lead\ntrail  ":  "lead",
		"\n\nafter blanks": "after blanks",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitDetail(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// No "; " — version/skill details stay on one line.
		{"0.2.14 \u2192 0.2.16 (restart your agent to apply)", []string{"0.2.14 \u2192 0.2.16 (restart your agent to apply)"}},
		{"11 skills -> /a/b", []string{"11 skills -> /a/b"}},
		// Multi-clause error wraps at "; ", keeping the ";" on the line it ends.
		{"foo (stale?); remove it and re-run", []string{"foo (stale?);", "remove it and re-run"}},
		{"a; b; c", []string{"a;", "b;", "c"}},
	}
	for _, tc := range cases {
		got := splitDetail(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("splitDetail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A wrapped detail's continuation lines must indent to the detail column so they
// stay aligned under the first clause, and the first line must keep its ";".
func TestRenderReport_WrapsAndAlignsDetail(t *testing.T) {
	r := Report{Results: []Result{{
		Harness: "opencode",
		Status:  StatusDetected,
		Detail:  "install failed: /x/y exists but is not a directory (stale symlink?); remove it and re-run",
	}}}
	lines := strings.Split(strings.TrimRight(RenderReportString(r, false), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected the detail to wrap onto a second line:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.HasSuffix(lines[0], "(stale symlink?);") {
		t.Errorf("first line should end at the clause boundary with ';':\n%q", lines[0])
	}
	if lines[1] != detailIndent+"remove it and re-run" {
		t.Errorf("continuation not aligned to detail column:\n%q\nwant %q", lines[1], detailIndent+"remove it and re-run")
	}
	// The continuation must line up exactly under where the detail starts on row 0.
	if got := strings.Index(lines[0], "install failed:"); got != len(detailIndent) {
		t.Errorf("detail column = %d, but continuation indent = %d", got, len(detailIndent))
	}
}

// TestOutcome locks the install/update/up-to-date classification: version-bearing
// harnesses report the real delta; file-drop harnesses fall back to the
// pre-install boolean; an unreadable version degrades to a fresh install.
func TestOutcome(t *testing.T) {
	marketplace := func(post string) harness {
		return harness{name: "claude-code", version: func(environ) (string, string) { return post, "" }}
	}
	skills := harness{name: "cursor"} // version nil

	cases := []struct {
		name         string
		h            harness
		preVer       string
		wasInstalled bool
		detail       string
		wantStatus   Status
		wantDetail   string
	}{
		{"marketplace fresh", marketplace("0.2.16"), "", false, "marketplace: x", StatusInstalled, "0.2.16 (restart your agent to apply)"},
		{"marketplace updated", marketplace("0.2.16"), "0.2.14", true, "marketplace: x", StatusUpdated, "0.2.14 \u2192 0.2.16 (restart your agent to apply)"},
		{"marketplace current", marketplace("0.2.16"), "0.2.16", true, "marketplace: x", StatusUpToDate, "already at 0.2.16"},
		{"marketplace version unreadable", marketplace(""), "", false, "marketplace: x", StatusInstalled, "marketplace: x (restart your agent to apply)"},
		{"skills fresh", skills, "", false, "2 skills -> dir", StatusInstalled, "2 skills -> dir"},
		{"skills updated", skills, "", true, "2 skills -> dir", StatusUpdated, "2 skills -> dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := outcome(tc.h, environ{}, tc.preVer, tc.wasInstalled, tc.detail)
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

func TestVersionReaders(t *testing.T) {
	home := t.TempDir()
	seedClaudeVersions(t, home, "0.2.14", "0.2.16")
	seedCodexVersions(t, home, "0.2.15", "0.2.16")
	e := environ{home: home, getenv: emptyEnv, lookPath: noBins}

	if inst, avail := claudeVersions(e); inst != "0.2.14" || avail != "0.2.16" {
		t.Errorf("claudeVersions = %q/%q, want 0.2.14/0.2.16", inst, avail)
	}
	if inst, avail := codexVersions(e); inst != "0.2.15" || avail != "0.2.16" {
		t.Errorf("codexVersions = %q/%q, want 0.2.15/0.2.16", inst, avail)
	}
	// Missing files read as empty, never an error/panic.
	if inst, avail := claudeVersions(environ{home: t.TempDir()}); inst != "" || avail != "" {
		t.Errorf("missing files = %q/%q, want empty", inst, avail)
	}
}

// codexInstalledVersion picks the highest valid-semver dir and ignores stray
// non-version directories a stale cache may leave behind.
func TestCodexInstalledVersionPicksHighest(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".codex", "plugins", "cache", marketplaceName, "volcano")
	for _, d := range []string{"0.2.9", "0.2.16", "0.2.10", "tmp-garbage"} {
		mustMkdir(t, filepath.Join(base, d))
	}
	if got := codexInstalledVersion(environ{home: home}); got != "0.2.16" {
		t.Fatalf("codexInstalledVersion = %q, want 0.2.16", got)
	}
}

// seedClaudeVersions writes claude-code's installed registry and cached
// marketplace manifest so the version readers see installed/available.
func seedClaudeVersions(t *testing.T, home, installed, available string) {
	t.Helper()
	reg := filepath.Join(home, ".claude", "plugins")
	mustMkdir(t, reg)
	regJSON := `{"version":2,"plugins":{"` + pluginRef + `":[{"version":"` + installed + `"}]}}`
	if err := os.WriteFile(filepath.Join(reg, "installed_plugins.json"), []byte(regJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	mpDir := filepath.Join(reg, "marketplaces", marketplaceName)
	mustMkdir(t, mpDir)
	if err := os.WriteFile(filepath.Join(mpDir, ".release-please-manifest.json"), []byte(`{".":"`+available+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seedCodexVersions writes codex's version cache dir and cached marketplace
// manifest so the version readers see installed/available.
func seedCodexVersions(t *testing.T, home, installed, available string) {
	t.Helper()
	mustMkdir(t, filepath.Join(home, ".codex", "plugins", "cache", marketplaceName, "volcano", installed))
	mpDir := filepath.Join(home, ".codex", ".tmp", "marketplaces", marketplaceName)
	mustMkdir(t, mpDir)
	if err := os.WriteFile(filepath.Join(mpDir, ".release-please-manifest.json"), []byte(`{".":"`+available+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, wantSubstr string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(b), wantSubstr) {
		t.Errorf("%s: %q does not contain %q", path, string(b), wantSubstr)
	}
}

// TestRenderReport_Footer locks the summary wording for the modes the reviewers
// flagged: a dry run must not claim an install happened, and only a genuine
// no-detection fallback may say no harness was detected.
func TestRenderReport_Footer(t *testing.T) {
	cases := []struct {
		name   string
		report Report
		want   string
	}{
		{
			name:   "dry-run planned",
			report: Report{Results: []Result{{Harness: "cursor", Status: StatusPlanned}, {Harness: "codex", Status: StatusSkipped}}},
			want:   "Would install Volcano for 1 harness(es).",
		},
		{
			name:   "dry-run manual fallback",
			report: Report{ManualFallback: true, Results: []Result{{Harness: "manual", Status: StatusPlanned}}},
			want:   "would install Volcano skills to ~/.volcano/skills.",
		},
		{
			name:   "autodetect with a detected install failure",
			report: Report{Results: []Result{{Harness: "claude-code", Status: StatusInstalled}, {Harness: "cursor", Status: StatusDetected}}},
			want:   "Installed Volcano for 1 harness(es); 1 detected but failed to install.",
		},
		{
			name:   "only detected install failures",
			report: Report{Results: []Result{{Harness: "cursor", Status: StatusDetected}}},
			want:   "Detected 1 harness(es), but installation failed.",
		},
		{
			name:   "manual requested is not a fallback",
			report: Report{Results: []Result{{Harness: "manual", Status: StatusInstalled}}},
			want:   "Installed Volcano for 1 harness(es).",
		},
		{
			name:   "no-detection fallback installed",
			report: Report{ManualFallback: true, Results: []Result{{Harness: "manual", Status: StatusInstalled}}},
			want:   "No coding-agent harness detected — installed Volcano skills to ~/.volcano/skills.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			RenderReport(&b, tc.report)
			if !strings.Contains(b.String(), tc.want) {
				t.Errorf("footer:\n%s\nwant substring %q", b.String(), tc.want)
			}
		})
	}
}

// TestRun_OnlyBestEffortDowngradesFailure locks the failure-policy split: an
// explicit Only target that fails is a hard failure, but the same set run with
// BestEffort (the interactive default selection) downgrades to detected and
// exits 0 — matching the autodetect/--yes path over the identical harness set.
func TestRun_OnlyBestEffortDowngradesFailure(t *testing.T) {
	badSkills := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badSkills.Close)
	base := Options{
		HTTPDoer:      badSkills.Client(),
		SkillsBaseURL: badSkills.URL,
		HomeDir:       t.TempDir(),
		Getenv:        emptyEnv,
		LookPath:      noBins,
		Only:          []string{"cursor"},
	}

	strict, err := Run(context.Background(), base)
	if err != nil {
		t.Fatalf("Run strict: %v", err)
	}
	if got := statusOf(strict, "cursor"); got != StatusFailed {
		t.Fatalf("strict cursor status = %q, want failed", got)
	}
	if !strict.Failed() {
		t.Fatal("strict --harness run should report Failed()")
	}

	best := base
	best.BestEffort = true
	be, err := Run(context.Background(), best)
	if err != nil {
		t.Fatalf("Run best-effort: %v", err)
	}
	if got := statusOf(be, "cursor"); got != StatusDetected {
		t.Fatalf("best-effort cursor status = %q, want detected", got)
	}
	if be.Failed() {
		t.Fatal("best-effort run must not report Failed()")
	}
}

// TestStatusMark locks the user-visible report vocabulary so the rename to
// full words can't silently regress.
func TestStatusMark(t *testing.T) {
	cases := map[Status]string{
		StatusInstalled: "[installed]",
		StatusDetected:  "[detected]",
		StatusFailed:    "[failed]",
		StatusPlanned:   "[planned]",
		StatusSkipped:   "[skipped]",
	}
	for status, want := range cases {
		if got := statusMark(status); got != want {
			t.Errorf("statusMark(%q) = %q, want %q", status, got, want)
		}
	}
}

// TestRenderReportAligns guards column alignment: the widest mark ([installed],
// 11 cols) must not push the harness column out relative to a shorter mark.
func TestRenderReportAligns(t *testing.T) {
	var b strings.Builder
	RenderReport(&b, Report{Results: []Result{
		{Harness: "claude-code", Status: StatusInstalled},
		{Harness: "codex", Status: StatusFailed, Detail: "boom"},
	}})
	out := b.String()
	installedAt := strings.Index(out, "claude-code")
	failedAt := strings.Index(out, "codex")
	if installedAt < 0 || failedAt < 0 {
		t.Fatalf("missing harness rows:\n%s", out)
	}
	// Both harness names start at the same column offset within their line.
	col := func(idx int) int { return idx - strings.LastIndex(out[:idx], "\n") }
	if col(installedAt) != col(failedAt) {
		t.Errorf("harness columns misaligned: [installed] row=%d, [failed] row=%d\n%s",
			col(installedAt), col(failedAt), out)
	}
}
