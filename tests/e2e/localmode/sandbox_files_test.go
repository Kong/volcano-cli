package localmode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// TestLocalModeE2ESandboxFiles lists a session's workspace through the CLI:
// exact entries with types, modes, sizes and digests, a symlink reported and
// not followed, paging, why a hard-linked file has no digest, a directory the
// session user can't list and names the listing can't carry, and the refusals
// for a path that is not a directory or can't be listed. It has its own stack
// and project, so a failure here does not stop the smoke's later checks.
func TestLocalModeE2ESandboxFiles(t *testing.T) {
	if os.Getenv("VOLCANO_LOCALMODE_E2E") != "1" {
		t.Skip("set VOLCANO_LOCALMODE_E2E=1 to run disposable Docker acceptance")
	}
	requireDocker(t)
	binary := buildLocalModeE2EBinary(t)
	env, dir := localModeE2EEnv(t), localModeE2EProjectDir(t)
	stopLocalModeE2EStack(t, binary, env, dir)
	registerLocalModeE2EStopClean(t, binary, env, dir)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "start"), "Volcano is ready for local development.")

	run := func(args ...string) string {
		t.Helper()
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	listing := func(args ...string) apiclient.SandboxFileListResult {
		t.Helper()
		var result apiclient.SandboxFileListResult
		output := run(append([]string{"files", "list"}, append(args, "--json")...)...)
		require.NoError(t, json.Unmarshal([]byte(output), &result), output)
		return result
	}
	list := func(args ...string) []apiclient.SandboxFileEntry {
		t.Helper()
		return listing(args...).Entries
	}
	var session struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("run", "--preset", "python3.12", "--duration", "600", "--json")), &session))
	require.NotEmpty(t, session.ID)
	t.Cleanup(func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "terminate", session.ID)
	})
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"running"`, "sandboxes", "get", session.ID, "--json")

	const (
		mainPy = "print('hello from the sandbox')\n"
		data   = "data\n"
		script = "#!/bin/sh\necho run\n"
	)
	run("exec", session.ID, "--", "mkdir", "-p", "/workspace/src/pkg")
	writeLocalModeE2ESandboxFile(t, binary, env, dir, session.ID, "/workspace/src/main.py", mainPy)
	writeLocalModeE2ESandboxFile(t, binary, env, dir, session.ID, "/workspace/src/pkg/data.txt", data)
	writeLocalModeE2ESandboxFile(t, binary, env, dir, session.ID, "/workspace/src/run.sh", script)
	run("exec", session.ID, "--", "sh", "-c", strings.Join([]string{
		"chmod 755 /workspace/src /workspace/src/pkg /workspace/src/run.sh",
		"chmod 644 /workspace/src/main.py",
		"chmod 600 /workspace/src/pkg/data.txt",
		"ln -s pkg /workspace/src/pkg-link",
	}, " && "))

	digest := func(content string) *string {
		sum := sha256.Sum256([]byte(content))
		value := hex.EncodeToString(sum[:])
		return &value
	}
	// Paths compare one element at a time, so src/pkg/data.txt sorts before
	// src/pkg-link although "-" sorts before "/". The symlink's target is not
	// walked.
	want := []apiclient.SandboxFileEntry{
		{Path: "src/main.py", Type: apiclient.File, Size: int64(len(mainPy)), Mode: 0o644, Sha256: digest(mainPy)},
		{Path: "src/pkg", Type: apiclient.Directory, Mode: 0o755},
		{Path: "src/pkg/data.txt", Type: apiclient.File, Size: int64(len(data)), Mode: 0o600, Sha256: digest(data)},
		{Path: "src/pkg-link", Type: apiclient.Symlink, Mode: 0o777},
		{Path: "src/run.sh", Type: apiclient.File, Size: int64(len(script)), Mode: 0o755, Sha256: digest(script)},
	}
	assert.Equal(t, want, list(session.ID, "src", "--sha256"))
	assert.Equal(t, want, list(session.ID, "/workspace/src", "--sha256", "--limit", "2"), "every page, in order")

	withoutDigests := make([]apiclient.SandboxFileEntry, 0, len(want))
	for _, entry := range want {
		entry.Sha256 = nil
		withoutDigests = append(withoutDigests, entry)
	}
	direct := []apiclient.SandboxFileEntry{withoutDigests[0], withoutDigests[1], withoutDigests[3], withoutDigests[4]}
	assert.Equal(t, direct, list(session.ID, "src", "--recursive=false"))

	// The workspace root can hold more than this test wrote.
	var underSrc []apiclient.SandboxFileEntry
	for _, entry := range list(session.ID) {
		if entry.Path == "src" || strings.HasPrefix(entry.Path, "src/") {
			underSrc = append(underSrc, entry)
		}
	}
	assert.Equal(t, append([]apiclient.SandboxFileEntry{{Path: "src", Type: apiclient.Directory, Mode: 0o755}}, withoutDigests...), underSrc)

	requireContains(t, run("files", "list", session.ID, "src"), "symlink    0777             -  src/pkg-link")

	// A file with more than one hard link gets no digest and says why.
	run("exec", session.ID, "--", "sh", "-c", strings.Join([]string{
		"mkdir /workspace/linked",
		"echo linked > /workspace/linked/a",
		"chmod 644 /workspace/linked/a",
		"ln /workspace/linked/a /workspace/linked/b",
	}, " && "))
	unreadable := apiclient.Unreadable
	assert.Equal(t, []apiclient.SandboxFileEntry{
		{Path: "linked/a", Type: apiclient.File, Size: int64(len("linked\n")), Mode: 0o644, Sha256Skipped: &unreadable},
		{Path: "linked/b", Type: apiclient.File, Size: int64(len("linked\n")), Mode: 0o644, Sha256Skipped: &unreadable},
	}, list(session.ID, "linked", "--sha256"))
	requireContains(t, run("files", "list", session.ID, "linked", "--sha256"), "skipped: unreadable")

	for _, path := range []string{"src/main.py", "src/pkg-link", "src/missing"} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "files", "list", session.ID, path)
		require.Error(t, err, "listing %s must fail\n%s", path, output)
		requireContains(t, output, `"`+path+`" is not a directory in the session's workspace`)
	}

	// The session user can't list a mode-000 directory: the listing marks it
	// unreadable, leaves out what it holds, and walks on. Names that are not
	// UTF-8 are counted in their directory, never listed.
	run("exec", session.ID, "--", "sh", "-c", strings.Join([]string{
		"mkdir -p /workspace/perm/locked /workspace/perm/open",
		"echo hidden > /workspace/perm/locked/hidden.txt",
		"echo open > /workspace/perm/open/visible.txt",
		`touch "/workspace/perm/$(printf 'bad-\377')" "/workspace/perm/open/$(printf 'bad-\376')"`,
		"chmod 755 /workspace/perm /workspace/perm/open",
		"chmod 644 /workspace/perm/open/visible.txt",
		"chmod 000 /workspace/perm/locked",
	}, " && "))
	yes, one := true, 1
	perm := listing(session.ID, "perm")
	assert.Equal(t, session.ID, perm.SessionId.String())
	assert.Equal(t, []apiclient.SandboxFileEntry{
		{Path: "perm/locked", Type: apiclient.Directory, Mode: 0, Unreadable: &yes},
		{Path: "perm/open", Type: apiclient.Directory, Mode: 0o755, UnsupportedNames: &one},
		{Path: "perm/open/visible.txt", Type: apiclient.File, Size: int64(len("open\n")), Mode: 0o644},
	}, perm.Entries, "the walk continues past the unreadable directory")
	assert.Equal(t, &one, perm.UnsupportedNames, "the name directly in perm")

	human := runVolcanoLocalModeE2E(t, binary, env, dir, "sandboxes", "files", "list", session.ID, "perm")
	requireContains(t, human, "directory  0000    unreadable  perm/locked\n")
	requireContains(t, human, "directory  0755    incomplete  perm/open\n")
	requireContains(t, human, "file       0644             5  perm/open/visible.txt\n")
	requireNotContains(t, human, "hidden.txt")
	requireContains(t, human, "Warning: The listing is incomplete.\n")
	requireContains(t, human, "1 directory marked unreadable was not entered")
	requireContains(t, human, "2 names were skipped, with everything below them, because they are not UTF-8 or their paths would pass 4096 bytes (1 in the listed directory, 1 in 1 directory marked incomplete).")

	// Only sandbox_path_unreadable gets this message: the directory itself,
	// and a path that passes through it.
	for _, path := range []string{"perm/locked", "perm/locked/hidden.txt"} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "files", "list", session.ID, path)
		require.Error(t, err, "listing %s must fail\n%s", path, output)
		requireContains(t, output, `"`+path+`" can't be listed: the session user lacks read or execute permission`)
	}
}

func writeLocalModeE2ESandboxFile(t *testing.T, binary string, env []string, dir, session, path, content string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "sandboxes", "files", "write", session, path)
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(content)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
}
