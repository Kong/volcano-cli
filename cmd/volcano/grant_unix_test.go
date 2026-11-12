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
	cliconfig.ResetAdoptedTokenForTest()
	t.Cleanup(cliconfig.ResetAdoptedTokenForTest)
	run := runAsWorkerWith(t, func() {
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
	}, func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusOK, `{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`)
	}, "cloud", "functions", "list")

	assert.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, []string{"Bearer " + testGrant}, run.auth)
	assert.Empty(t, os.Getenv("VOLCANO_TOKEN"))
}
