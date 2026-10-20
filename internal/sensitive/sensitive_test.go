package sensitive

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrap(t *testing.T) {
	require.NoError(t, Wrap(nil))

	inner := errors.New(`unterminated quoted value "hunter2`)
	err := fmt.Errorf("failed to read volcano.env: %w", Wrap(inner))

	assert.Equal(t, `failed to read volcano.env: unterminated quoted value "hunter2`, err.Error(), "the terminal still shows the text")
	require.ErrorIs(t, err, inner)
	assert.Equal(t, inner.Error(), Text(err))
	assert.Empty(t, Text(inner))
}

func TestContains(t *testing.T) {
	code := func(err error) string {
		if err.Error() == "coded" {
			return "c"
		}
		return ""
	}
	assert.True(t, Contains(fmt.Errorf("x: %w", Wrap(errors.New("coded"))), code))
	assert.False(t, Contains(Wrap(errors.New("plain")), code))
	assert.False(t, Contains(errors.New("coded"), code), "outside a sensitive error")
}
