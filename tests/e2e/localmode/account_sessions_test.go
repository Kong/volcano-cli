package localmode

import "testing"

// requireLocalModeLeavesAccountSessionsToTheCloud points the account session
// commands at the local stack, which keeps no sign-in sessions, and checks each
// gives the server's reason.
func requireLocalModeLeavesAccountSessionsToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	local := append(append([]string{}, env...),
		"VOLCANO_API_URL="+info.APIURL,
		"VOLCANO_TOKEN="+info.UserToken,
	)
	for _, args := range [][]string{
		{"account", "sessions", "list"},
		{"account", "sessions", "revoke", "00000000-0000-4000-8000-000000000000"},
		{"account", "sessions", "revoke", "--all-others", "--yes"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, local, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %v to fail against the local stack\n%s", args, output)
		}
		requireContains(t, output, "account sessions are cloud-only")
	}
}
