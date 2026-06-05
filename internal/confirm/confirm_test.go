package confirm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteConfirmsYesInputs(t *testing.T) {
	for _, input := range []string{"y\n", "yes\n", " YES \n"} {
		var out bytes.Buffer

		confirmed, err := Delete(bytes.NewBufferString(input), &out, "database", "app")
		require.NoError(t, err)
		assert.True(t, confirmed)
		assert.Contains(t, out.String(), destructiveDeleteMessage)
		assert.Contains(t, out.String(), "Delete database 'app'?")
		assert.NotContains(t, out.String(), "Delete cancelled.")
	}
}

func TestDeleteConfirmsMultipleResources(t *testing.T) {
	var out bytes.Buffer

	confirmed, err := Delete(bytes.NewBufferString("yes\n"), &out, "variables", "API_KEY", "SECRET_KEY")
	require.NoError(t, err)
	assert.True(t, confirmed)
	assert.Contains(t, out.String(), destructiveDeleteMessage)
	assert.Contains(t, out.String(), "Delete 2 variables: 'API_KEY', 'SECRET_KEY'?")
	assert.NotContains(t, out.String(), "Delete cancelled.")
}

func TestDeleteCancelsOtherInputs(t *testing.T) {
	for _, input := range []string{"n\n", "\n", ""} {
		var out bytes.Buffer

		confirmed, err := Delete(bytes.NewBufferString(input), &out, "variable", "API_KEY")
		require.NoError(t, err)
		assert.False(t, confirmed)
		assert.Contains(t, out.String(), destructiveDeleteMessage)
		assert.Contains(t, out.String(), "Delete variable 'API_KEY'?")
		assert.Contains(t, out.String(), "Delete cancelled.")
	}
}

func TestDeleteReturnsReadError(t *testing.T) {
	var out bytes.Buffer

	confirmed, err := Delete(errReader{}, &out, "variable", "API_KEY")
	require.ErrorContains(t, err, "read failed")
	assert.False(t, confirmed)
	assert.NotContains(t, out.String(), "Delete cancelled.")
}

func TestDeleteAcceptsWrappedEOF(t *testing.T) {
	reader := onceReader{
		value: "yes",
		err:   fmt.Errorf("read input: %w", io.EOF),
	}

	confirmed, err := Delete(&reader, io.Discard, "database", "app")
	require.NoError(t, err)
	assert.True(t, confirmed)
}

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

type onceReader struct {
	value string
	err   error
	read  bool
}

func (r *onceReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	return copy(p, r.value), r.err
}
