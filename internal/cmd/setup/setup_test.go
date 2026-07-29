package setupcmd

import (
	"io"
	"strings"
	"testing"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// TestSetupFlagContract locks the public CLI contract of the rename: setup
// registers --agent, no longer accepts the old --harness, and --agent still
// can't be combined with --manual or --yes. These checks execute at cobra's
// parse/validate stage (before RunE), so they never touch the network.
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

	// --agent keeps the mutual-exclusion guards under its new name.
	for _, other := range []string{"--manual", "--yes"} {
		err := execSetup(t, "--agent", "claude-code", other)
		if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
			t.Errorf("--agent + %s should be mutually exclusive, got %v", other, err)
		}
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
