package localmode

import (
	"strings"
	"testing"
)

// requireLocalModeRefusesFunctionPreviews creates a preview of the deployed
// hello function against the local stack and checks the refusal says previews
// are unavailable: local functions run without the workflow a preview's own
// Lambda and its promotion are built on. Reading a function's previews still
// works and finds none.
func requireLocalModeRefusesFunctionPreviews(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	args := []string{"functions", "preview", "create", "hello", "--name", "next"}
	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
	if err == nil {
		t.Fatalf("expected volcano %s to fail against the local stack\n%s", strings.Join(args, " "), output)
	}
	requireContains(t, output, "function candidates are not available")
	requireNotContains(t, output, "Preview 'next' deployment started")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "preview", "list", "hello"), "No previews.")
}
