package localmode

import (
	"strings"
	"testing"
)

// requireLocalModeLeavesGitToTheCloud points the commands that change one part
// of a binding at the local stack and checks they say why they cannot work
// there: the local stack has no GitHub App, so no repository can be connected
// for a root directory or a production branch to belong to.
func requireLocalModeLeavesGitToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	local := append(append([]string{}, env...),
		"VOLCANO_API_URL="+info.APIURL,
		"VOLCANO_TOKEN="+info.UserToken,
		"VOLCANO_PROJECT_ID="+info.ProjectID,
	)

	for _, args := range [][]string{
		{"git", "root-directory", "apps/web"},
		{"git", "production-branch", "release"},
	} {
		command := "volcano " + strings.Join(args, " ")
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, local, dir, args...)
		if err == nil {
			t.Fatalf("expected %s to fail against the local stack\n%s", command, output)
		}
		requireContains(t, output, "git provider integration is not configured on this API")
		requireContains(t, output, "If this is local mode, that is expected")
		requireNotContains(t, output, "Connect one with")
	}
}
