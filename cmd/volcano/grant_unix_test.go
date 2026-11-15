//go:build unix

package main

import (
	"net/http"
	"os"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// The worker can pass the grant on a descriptor instead of VOLCANO_TOKEN, so
// it is never in the CLI's environment. main adopts it before the command runs.
func TestGrantFromATokenDescriptor(t *testing.T) {
	run := runAsWorkerWith(t, func() { adoptGrantFromADescriptor(t) }, func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusOK, `{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`)
	}, "cloud", "functions", "list")

	assert.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, []string{"Bearer " + testGrant}, run.auth)
	assert.Empty(t, os.Getenv("VOLCANO_TOKEN"))
}

// A refusal that says which variable to unset names VOLCANO_TOKEN_FD when the
// token came from it: unsetting VOLCANO_TOKEN would change nothing.
func TestGrantFromATokenDescriptorIsNamedInSignInHints(t *testing.T) {
	for _, args := range [][]string{
		{"cloud", "lavaflow", "disable"},
		{"cloud", "durable", "approvals", "approve", "11111111-1111-4111-8111-111111111111", "--yes"},
	} {
		t.Run(args[1]+" "+args[2], func(t *testing.T) {
			run := runAsWorkerWith(t, func() { adoptGrantFromADescriptor(t) }, func(w http.ResponseWriter, _ *http.Request) {
				respondExitTestJSON(w, http.StatusForbidden, `{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`)
			}, args...)

			assert.Equal(t, exitPermissionDenied, run.code, run.stderr)
			assert.Contains(t, run.stderr, cliconfig.TokenFDEnv)
			assert.NotContains(t, run.stderr, "VOLCANO_TOKEN ")
		})
	}
}

// adoptGrantFromADescriptor puts the grant on a pipe, names it in
// VOLCANO_TOKEN_FD and adopts it, as main does at startup.
func adoptGrantFromADescriptor(t *testing.T) {
	t.Helper()
	cliconfig.ResetAdoptedTokenForTest()
	t.Cleanup(cliconfig.ResetAdoptedTokenForTest)
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(testGrant + "\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	// A duplicate no *os.File owns, so only AdoptToken closes it.
	fd, err := syscall.Dup(int(r.Fd()))
	require.NoError(t, err)
	require.NoError(t, r.Close())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv(cliconfig.TokenFDEnv, strconv.Itoa(fd))
	require.NoError(t, cliconfig.AdoptToken())
}
