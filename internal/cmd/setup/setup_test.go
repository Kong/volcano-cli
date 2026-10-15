package setupcmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/setup"
)

// TestSetupFlagContract locks the public CLI contract of the rename: setup
// registers --agent, no longer accepts the old --harness, --agent still can't
// be combined with --manual, and --yes composes with either targeting flag
// (VOL-640: --yes is a no-op alongside --agent/--manual, not a conflict, since
// both are already non-interactive on their own). These checks execute at
// cobra's parse/validate stage (before RunE), so they never touch the network.
func TestSetupFlagContract(t *testing.T) {
	cmd := New(cliruntime.Deps{})
	if cmd.Flags().Lookup("agent") == nil {
		t.Error("--agent flag must be registered")
	}
	if cmd.Flags().Lookup("harness") != nil {
		t.Error("old --harness flag must be gone")
	}

	// --harness is now an unknown flag, rejected at parse time.
	if err := execSetup(t, "--harness", "claude-code"); err == nil ||
		!strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("--harness should be rejected as unknown, got %v", err)
	}

	// --agent and --manual still contradict (Run gives --agent precedence).
	if err := execSetup(t, "--agent", "claude-code", "--manual"); err == nil ||
		!strings.Contains(err.Error(), "none of the others can be") {
		t.Errorf("--agent + --manual should be mutually exclusive, got %v", err)
	}

	// --yes composes with --agent and --manual: parsing succeeds (the
	// dry-run guard keeps this test network-free once past validation).
	for _, args := range [][]string{
		{"--agent", "claude-code", "--yes", "--dry-run"},
		{"--manual", "--yes", "--dry-run"},
	} {
		if err := execSetup(t, args...); err != nil {
			t.Errorf("%v should be accepted, got %v", args, err)
		}
	}
}

func TestPromptHarnessesNoAgents(t *testing.T) {
	var out bytes.Buffer
	cmd := New(cliruntime.Deps{})
	cmd.SetOut(&out)
	selected, cancelled, err := promptHarnesses(cmd, setup.Options{
		HomeDir:  t.TempDir(),
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Getenv:   func(string) string { return "" },
	}, false)
	if err != nil || cancelled || len(selected) != 0 {
		t.Fatalf("selected=%v cancelled=%v err=%v", selected, cancelled, err)
	}
	want := "No coding agents found. Supported agents include Claude Code, Codex, and Cursor. Set one up, then run `volcano setup` again.\n"
	if got := out.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// execSetup runs setup with args through cobra and returns the resulting error.
// A fresh command per call avoids leaking flag state between assertions; output
// is discarded so failing runs don't print usage into the test log.
func execSetup(t *testing.T, args ...string) error {
	t.Helper()
	cmd := New(cliruntime.Deps{})
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.Execute()
}

// Under a coding agent, setup targets that agent alone instead of every
// harness on the machine, and says so.
func TestSetupTargetsTheCallingAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("OPENCODE", "")
	t.Setenv("PI_CODING_AGENT", "")

	var out bytes.Buffer
	cmd := New(cliruntime.Deps{})
	cmd.SetArgs([]string{"--yes", "--dry-run"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !strings.Contains(out.String(), "Running inside claude-code: installing for claude-code only.") {
		t.Errorf("missing calling-agent note:\n%s", out.String())
	}
	for _, other := range []string{"codex", "cursor", "opencode"} {
		if strings.Contains(out.String(), other) {
			t.Errorf("report mentions %s, want claude-code only:\n%s", other, out.String())
		}
	}
}
