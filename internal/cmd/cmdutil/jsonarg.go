package cmdutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ParseJSONObject reads a flag value that carries a JSON object, either inline
// or as the path to a file holding it. subject names the flag in errors, so
// they read as "payload must be a JSON object" or "input must be a JSON
// object".
func ParseJSONObject(subject, value string) (map[string]any, error) {
	data := []byte(value)
	if info, err := os.Stat(value); err == nil {
		// Only a regular file is read. A directory, a device or a fifo named here
		// is a mistake in the command line, and reading one of the last two
		// blocks or returns something that is not the caller's JSON either way.
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s file %q must be a regular file", subject, value)
		}
		fileBytes, readErr := os.ReadFile(value)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read %s file %q: %w", subject, value, readErr)
		}
		data = fileBytes
	}

	object, err := decodeJSONObject(data)
	if err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", subject, err)
	}
	// `null` decodes into a nil map without an error, so it reaches the caller as
	// an object with no fields. That is not what the flag asked for, and it is
	// indistinguishable from omitting the flag by the time it is sent.
	if object == nil {
		return nil, fmt.Errorf("%s must be a JSON object, not null", subject)
	}
	return object, nil
}

// decodeJSONObject decodes the object with its numbers left as they were
// written.
//
// Plain decoding turns every JSON number into a float64, and this value is
// re-encoded before it is sent: an id past 2^53 comes out as a different id, and
// the execution runs against something the caller never asked for. json.Number
// keeps the literal, so what is sent is what was typed.
//
// A decoder reads one value and stops, unlike json.Unmarshal, so trailing JSON
// has to be refused here or `{} {}` would quietly parse as the first object.
func decodeJSONObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errTrailingJSON
	}
	return object, nil
}

var errTrailingJSON = errors.New("unexpected data after the JSON object")
