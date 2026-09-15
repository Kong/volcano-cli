package cmdutil

import (
	"encoding/json"
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

	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
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
