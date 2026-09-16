package cmdutil

import (
	"encoding/json"
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
	assert.JSONEq(t, `{"order_id":4417}`, remarshal(t, inline))

	path := filepath.Join(t.TempDir(), "input.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"order_id":4418}`), 0o600))

	fromFile, err := ParseJSONObject("input", path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"order_id":4418}`, remarshal(t, fromFile))
}

// A durable start forwards the input as it was given, and the parsed value is
// re-encoded on the way. Decoded as float64, an id past 2^53 arrives as a
// different id and the execution runs for the wrong thing.
func TestParseJSONObjectKeepsLargeIntegersExact(t *testing.T) {
	input, err := ParseJSONObject("input",
		`{"order_id":9007199254740993,"total":1.5,"ref":"abc"}`)
	require.NoError(t, err)

	assert.JSONEq(t,
		`{"order_id":9007199254740993,"total":1.5,"ref":"abc"}`,
		remarshal(t, input),
		"what is sent has to be what was typed")
}

// A decoder reads one value and stops, so refusing what follows is explicit
// rather than inherited from json.Unmarshal.
func TestParseJSONObjectRejectsTrailingJSON(t *testing.T) {
	_, err := ParseJSONObject("input", `{"order_id":1} {"order_id":2}`)

	require.ErrorContains(t, err, "unexpected data after the JSON object")
}

// remarshal is the trip the value takes on its way to the API.
func remarshal(t *testing.T, object map[string]any) string {
	t.Helper()

	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	return string(encoded)
}
