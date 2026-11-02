package localmode

import (
	"strings"
	"testing"
)

// requireLocalModeLeavesLavaFlowToTheCloud checks that LavaFlow, which runs on
// Volcano's cloud service only, refuses in local mode with that reason and
// names the cloud command instead of asking the local API, which answers
// feature_unavailable.
func requireLocalModeLeavesLavaFlowToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	help := runVolcanoLocalModeE2E(t, binary, env, dir, "--help")
	requireNotContains(t, help, "lavaflow")

	for _, args := range [][]string{
		{"lavaflow", "status"},
		{"lavaflow", "enable"},
		{"lavaflow", "disable", "--json"},
		{"lavaflow", "settings", "set", "--autonomy", "diagnose_only"},
		{"lavaflow", "stats", "--json"},
		{"lavaflow", "list"},
		{"lavaflow", "show", "run-id"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail in local mode\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, `"lavaflow" is a cloud command`)
		requireContains(t, output, "LavaFlow runs on Volcano's cloud service, which local development doesn't run")
		requireContains(t, output, "volcano cloud lavaflow")
	}
}
