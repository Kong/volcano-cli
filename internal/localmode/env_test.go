package localmode

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalEnvOverridesReadsDotEnvLocal(t *testing.T) {
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte(`
	# ignored
	PLAIN=value
	DOUBLE="quoted"
	SINGLE='quoted-single'
	export EXPORTED=exported
	INLINE=kept # comment
	`), 0o600))

	envs, err := localEnvOverrides()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"DOUBLE=quoted",
		"EXPORTED=exported",
		"INLINE=kept",
		"PLAIN=value",
		"SINGLE=quoted-single",
	}, envs)
}

func TestLocalEnvOverridesErrorsOnMalformedDotEnvLocal(t *testing.T) {
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte("NO_EQUALS\n"), 0o600))

	_, err := localEnvOverrides()
	require.ErrorContains(t, err, "failed to read .env.local")
}
