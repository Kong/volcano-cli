package localmode

import (
	"strings"
	"testing"
)

// requireLocalModeLeavesLavaFlowToTheCloud checks that LavaFlow, which opens
// fix pull requests through a GitHub App local development does not run,
// refuses with that reason and names the cloud command instead of running.
func requireLocalModeLeavesLavaFlowToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	help := runVolcanoLocalModeE2E(t, binary, env, dir, "--help")
	requireNotContains(t, help, "lavaflow")

	for _, args := range [][]string{
		{"lavaflow", "status"},
		{"lavaflow", "enable"},
		{"lavaflow", "disable", "--json"},
		{"lavaflow", "runs", "list"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail in local mode\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, `"lavaflow" is a cloud command`)
		requireContains(t, output, "local development has no GitHub App")
		requireContains(t, output, "volcano cloud lavaflow")
	}
}
