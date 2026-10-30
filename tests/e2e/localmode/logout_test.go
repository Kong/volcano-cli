package localmode

import "testing"

// requireLocalModeLogoutKeepsNoServerSession logs in against the local stack
// and logs out. The local stack keeps no sign-in session, so it answers the
// revoke with 501 and logout only removes the login from this machine. It uses
// a HOME of its own, so the saved login it removes is not the smoke's.
func requireLocalModeLogoutKeepsNoServerSession(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	local := append(append([]string{}, env...),
		"HOME="+t.TempDir(),
		"VOLCANO_API_URL="+info.APIURL,
	)

	requireContains(t, runVolcanoLocalModeE2E(t, binary, local, dir, "login", "--token", info.UserToken), "Logged in successfully")
	output := runVolcanoLocalModeE2E(t, binary, local, dir, "logout")
	requireContains(t, output, "Logged out: this server keeps no sign-in session, so there was nothing to revoke")
	requireContains(t, output, "Removed the login from ~/.volcano/config.json.")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, local, dir, "logout"), "Logged out: no login was saved, so nothing was revoked")
}
