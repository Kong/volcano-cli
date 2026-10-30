package localmode

import (
	"strings"
	"testing"
)

// requireLocalModeLeavesGitToTheCloud points the git subcommands that change a
// connected repository's settings at the local stack and checks each says why
// it cannot work there: the local stack has no GitHub App, so no repository can
// be connected for a root directory, a production branch or a deploy setting
// to belong to. Source export says so too: local mode stores no source to
// export.
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
		{"git", "auto-deploy", "on"},
		{"git", "functions", "on"},
		{"git", "frontend", "web", "--root", "apps/web"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, local, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail against the local stack\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, "git provider integration is not configured on this API")
		requireContains(t, output, "If this is local mode, that is expected")
		requireNotContains(t, output, "Connect one with")
	}

	for _, args := range [][]string{
		{"git", "export", "--yes"},
		{"git", "export", "cancel", "--yes"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, local, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail against the local stack\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, "source export is not available on this API")
		requireContains(t, output, "If this is local mode, that is expected")
	}
}
