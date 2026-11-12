package config

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenPipe returns the read end of a pipe holding token, with the write end
// closed.
func tokenPipe(t *testing.T, token string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(token)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return r
}

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

func TestAdoptTokenReadsTheDescriptorAndClosesIt(t *testing.T) {
	adoptTokenTest(t)
	r := tokenPipe(t, DelegatedGrantPrefix+"from-fd\n")
	t.Setenv(TokenFDEnv, strconv.Itoa(int(r.Fd())))

	require.NoError(t, AdoptToken())

	assert.Equal(t, DelegatedGrantPrefix+"from-fd", (&Config{}).Token())
	assert.True(t, (&Config{}).TokenFromEnv())
	assert.Empty(t, (&Config{IgnoreEnv: true}).Token(), "local mode ignores it, as it ignores VOLCANO_TOKEN")
	_, set := os.LookupEnv(TokenFDEnv)
	assert.False(t, set, "the descriptor's number is not passed on")
	_, err := r.Read(make([]byte, 1))
	require.Error(t, err, "the descriptor is closed")
	assert.NotContains(t, envOfAChild(t), "from-fd")
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
	t.Run("empty", func(t *testing.T) {
		adoptTokenTest(t)
		r := tokenPipe(t, "  \n")
		t.Setenv(TokenFDEnv, strconv.Itoa(int(r.Fd())))
		require.ErrorContains(t, AdoptToken(), "no token on VOLCANO_TOKEN_FD")
	})
	t.Run("too long", func(t *testing.T) {
		adoptTokenTest(t)
		r := tokenPipe(t, strings.Repeat("a", maxTokenFDBytes+1))
		t.Setenv(TokenFDEnv, strconv.Itoa(int(r.Fd())))
		err := AdoptToken()
		require.ErrorContains(t, err, "is over")
		assert.NotContains(t, err.Error(), "aaaa")
	})
}
