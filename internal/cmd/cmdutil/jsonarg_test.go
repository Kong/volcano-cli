package cmdutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseJSONObjectRejectsNull(t *testing.T) {
	_, err := ParseJSONObject("input", "null")
	require.ErrorContains(t, err, "input must be a JSON object")
}

func TestParseJSONObjectRejectsNonRegularFiles(t *testing.T) {
	_, err := ParseJSONObject("input", t.TempDir())
	require.ErrorContains(t, err, "must be a regular file")
}
