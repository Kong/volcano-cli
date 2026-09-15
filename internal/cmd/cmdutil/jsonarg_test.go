package cmdutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The flag takes a JSON object, and two values got through that are not one.
//
// `null` decodes into a nil map without an error, so it arrived as an object
// with no fields — indistinguishable from omitting the flag by the time the
// request is built, and the omission is documented as starting an execution with
// no input at all. A path that is not a regular file was read as though it held
// the JSON: a directory failed with a read error nobody could act on, and a
// device or a fifo would block or hand back something that is not the caller's
// input.

func TestParseJSONObjectRejectsNull(t *testing.T) {
	_, err := ParseJSONObject("input", "null")

	require.ErrorContains(t, err, "input must be a JSON object, not null")
}

func TestParseJSONObjectRejectsAPathThatIsNotAFile(t *testing.T) {
	_, err := ParseJSONObject("input", t.TempDir())

	require.ErrorContains(t, err, "must be a regular file")
}

func TestParseJSONObjectReadsInlineAndFileObjects(t *testing.T) {
	inline, err := ParseJSONObject("input", `{"order_id":4417}`)
	require.NoError(t, err)
	assert.EqualValues(t, 4417, inline["order_id"])

	path := filepath.Join(t.TempDir(), "input.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"order_id":4418}`), 0o600))

	fromFile, err := ParseJSONObject("input", path)
	require.NoError(t, err)
	assert.EqualValues(t, 4418, fromFile["order_id"])
}
