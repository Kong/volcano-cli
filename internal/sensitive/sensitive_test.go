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
