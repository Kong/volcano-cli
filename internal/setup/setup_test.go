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
// VOLCANO_WEB_URL endpoints the CLI fetches from.
func skillsServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/skills/index.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,"skills":[
			{"name":"volcano-platform","path":"/skills/volcano-platform/SKILL.md"},
			{"name":"install-volcano","path":"/skills/install-volcano/SKILL.md"}]}`)
	})
	mux.HandleFunc("/skills/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "# Skill "+r.URL.Path+"\nVolcano skill content\n")
	})
	mux.HandleFunc("/AGENTS.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "# Volcano AGENTS.md\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func noBins(string) (string, error) { return "", errors.New("not found") }

func emptyEnv(string) string { return "" }

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return nil, f.err
}

func statusOf(r Report, harness string) Status {
	for _, res := range r.Results {
		if res.Harness == harness {
			return res.Status
		}
	}
	return Status("<absent>")
}

func TestRun_AutodetectInstallsDetected(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor"))
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))
	mustMkdir(t, filepath.Join(home, ".config", "opencode"))
	srv := skillsServer(t)

	report, err := Run(context.Background(), Options{
		HTTPDoer: srv.Client(),
		WebURL:   srv.URL,
		HomeDir:  home,
		Getenv:   emptyEnv,
		LookPath: noBins, // no claude/codex on PATH
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
	assertFile(t, filepath.Join(home, ".config", "opencode", "AGENTS.md"), "Volcano AGENTS.md")
}

func TestRun_MarketplaceHarnessShellsOut(t *testing.T) {
	home := t.TempDir()
	runner := &fakeRunner{}
	report, err := Run(context.Background(), Options{
		CommandRunner: runner,
		WebURL:        "http://example.invalid",
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
	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d, want 2: %v", len(runner.calls), runner.calls)
	}
	if got := strings.Join(runner.calls[0], " "); got != "claude plugin marketplace add "+marketplaceRepo {
		t.Errorf("first call = %q", got)
	}
	if got := strings.Join(runner.calls[1], " "); got != "claude plugin install "+pluginRef {
		t.Errorf("second call = %q", got)
	}
}

func TestRun_NoHarnessFallsBackToManual(t *testing.T) {
	home := t.TempDir()
	srv := skillsServer(t)
	report, err := Run(context.Background(), Options{
		HTTPDoer: srv.Client(),
		WebURL:   srv.URL,
		HomeDir:  home,
		Getenv:   emptyEnv,
		LookPath: noBins,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Manual {
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
		HTTPDoer: srv.Client(),
		WebURL:   srv.URL,
		HomeDir:  home,
		Getenv:   emptyEnv,
		LookPath: noBins,
		Manual:   true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Manual || len(report.Results) != 1 {
		t.Fatalf("expected single manual result, got %+v", report.Results)
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
		WebURL:        "http://example.invalid",
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

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

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
