package cmdutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ParseJSONObject reads a flag value that carries a JSON object, either inline
// or as the path to a file holding it. subject names the flag in errors, so
// they read as "payload must be a JSON object" or "input must be a JSON
// object".
func ParseJSONObject(subject, value string) (map[string]any, error) {
	data := []byte(value)
	if _, err := os.Stat(value); err == nil {
		fileBytes, readErr := ReadJSONFile(subject, value)
		if readErr != nil {
			return nil, readErr
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

// ReadJSONFile reads the file a flag names for its JSON. subject names the flag
// in errors.
func ReadJSONFile(subject, path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s file %q: %w", subject, path, err)
	}
	// Only a regular file is read. A directory, a device or a fifo named here
	// is a mistake in the command line, and reading one of the last two
	// blocks or returns something that is not the caller's JSON either way.
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s file %q must be a regular file", subject, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s file %q: %w", subject, path, err)
	}
	return data, nil
}

// ParseJSONValue checks that data is exactly one JSON value of any type and
// returns it as written, so numbers keep their literal.
func ParseJSONValue(subject string, data []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s must be JSON: %w", subject, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must be JSON: %w", subject, errTrailingJSONValue)
	}
	return value, nil
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
//
// Refused by requiring the stream to end rather than by asking More(), which
// reports "the next byte is not ] or }" and so answers false for a stray
// closing delimiter: `{"a":1}}` and `{"a":1}]` are both malformed and both
// used to parse as the object in front.
func decodeJSONObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errTrailingJSON
	}
	return object, nil
}

var (
	errTrailingJSON      = errors.New("unexpected data after the JSON object")
	errTrailingJSONValue = errors.New("unexpected data after the JSON value")
)
