package config

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adoptTokenTest(t *testing.T) {
	t.Helper()
	ResetAdoptedTokenForTest()
	t.Cleanup(ResetAdoptedTokenForTest)
	t.Setenv(envToken, "")
	t.Setenv(TokenFDEnv, "")
	require.NoError(t, os.Unsetenv(TokenFDEnv))
}

// envOfAChild returns the environment a process the CLI starts would get.
func envOfAChild(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return strings.Join(os.Environ(), "\n")
	}
	out, err := exec.CommandContext(t.Context(), "env").Output()
	require.NoError(t, err)
	return string(out)
}

// A grant in VOLCANO_TOKEN is taken out of the environment, so processes the
// CLI starts don't get it.
func TestAdoptTokenTakesAGrantOutOfTheEnvironment(t *testing.T) {
	adoptTokenTest(t)
	t.Setenv(envToken, " "+DelegatedGrantPrefix+"from-env\n")

	require.NoError(t, AdoptToken())

	assert.Equal(t, DelegatedGrantPrefix+"from-env", (&Config{}).Token())
	_, set := os.LookupEnv(envToken)
	assert.False(t, set)
	assert.NotContains(t, envOfAChild(t), "from-env")
}

// Any other token stays where it was: the telemetry flush reads an account
// token from VOLCANO_TOKEN.
func TestAdoptTokenLeavesOtherTokensInTheEnvironment(t *testing.T) {
	adoptTokenTest(t)
	t.Setenv(envToken, AccountTokenPrefix+"account")

	require.NoError(t, AdoptToken())

	assert.Equal(t, AccountTokenPrefix+"account", os.Getenv(envToken))
	assert.Equal(t, AccountTokenPrefix+"account", (&Config{}).Token())
}

func TestAdoptTokenRefusals(t *testing.T) {
	t.Run("both set", func(t *testing.T) {
		adoptTokenTest(t)
		t.Setenv(envToken, DelegatedGrantPrefix+"env")
		t.Setenv(TokenFDEnv, "3")
		require.ErrorContains(t, AdoptToken(), "set VOLCANO_TOKEN_FD or VOLCANO_TOKEN, not both")
	})
	t.Run("not a number", func(t *testing.T) {
		adoptTokenTest(t)
		t.Setenv(TokenFDEnv, "three")
		require.ErrorContains(t, AdoptToken(), "VOLCANO_TOKEN_FD must be a file descriptor number")
	})
	// Reading stdout or stderr would fail, and closing stderr would hide the
	// error that says so.
	for _, fd := range []string{"1", "2"} {
		t.Run("fd "+fd, func(t *testing.T) {
			adoptTokenTest(t)
			t.Setenv(TokenFDEnv, fd)
			require.ErrorContains(t, AdoptToken(), "VOLCANO_TOKEN_FD can't be "+fd+": that is stdout or stderr")
		})
	}
}
