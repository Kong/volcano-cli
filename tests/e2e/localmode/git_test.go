package localmode

import (
	"testing"
)

// requireLocalModeLeavesGitToTheCloud points `volcano git root-directory` at
// the local stack and checks it says why it cannot work there: the local stack
// has no GitHub App, so no repository can be connected for a root directory to
// belong to.
func requireLocalModeLeavesGitToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	local := append(append([]string{}, env...),
		"VOLCANO_API_URL="+info.APIURL,
		"VOLCANO_TOKEN="+info.UserToken,
		"VOLCANO_PROJECT_ID="+info.ProjectID,
	)

	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, local, dir, "git", "root-directory", "apps/web")
	if err == nil {
		t.Fatalf("expected volcano git root-directory to fail against the local stack\n%s", output)
	}
	requireContains(t, output, "git provider integration is not configured on this API")
	requireContains(t, output, "If this is local mode, that is expected")
	requireNotContains(t, output, "Connect one with")
}
