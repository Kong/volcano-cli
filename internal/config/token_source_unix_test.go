//go:build unix

package config

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenFD returns a descriptor that reads token, as a worker would pass one.
// It is a duplicate no *os.File owns, so only AdoptToken closes it: two owners
// of one descriptor would close it twice, the second time under whatever
// opened the number next.
func tokenFD(t *testing.T, token string) int {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString(token)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	fd, err := syscall.Dup(int(r.Fd()))
	require.NoError(t, err)
	require.NoError(t, r.Close())
	return fd
}

func TestAdoptTokenReadsTheDescriptorAndClosesIt(t *testing.T) {
	adoptTokenTest(t)
	fd := tokenFD(t, DelegatedGrantPrefix+"from-fd\n")
	t.Setenv(TokenFDEnv, strconv.Itoa(fd))

	require.NoError(t, AdoptToken())

	assert.Equal(t, DelegatedGrantPrefix+"from-fd", (&Config{}).Token())
	assert.True(t, (&Config{}).TokenFromEnv())
	assert.Empty(t, (&Config{IgnoreEnv: true}).Token(), "local mode ignores it, as it ignores VOLCANO_TOKEN")
	_, set := os.LookupEnv(TokenFDEnv)
	assert.False(t, set, "the descriptor's number is not passed on")
	_, err := syscall.Read(fd, make([]byte, 1))
	require.Error(t, err, "the descriptor is closed")
	assert.NotContains(t, envOfAChild(t), "from-fd")
}

func TestAdoptTokenRefusesAnEmptyOrOversizedRead(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		adoptTokenTest(t)
		t.Setenv(TokenFDEnv, strconv.Itoa(tokenFD(t, "  \n")))
		require.ErrorContains(t, AdoptToken(), "no token on VOLCANO_TOKEN_FD")
	})
	t.Run("too long", func(t *testing.T) {
		adoptTokenTest(t)
		t.Setenv(TokenFDEnv, strconv.Itoa(tokenFD(t, strings.Repeat("a", maxTokenFDBytes+1))))
		err := AdoptToken()
		require.ErrorContains(t, err, "is over")
		assert.NotContains(t, err.Error(), "aaaa")
	})
}
