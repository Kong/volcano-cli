package cmdutil

import (
	"encoding/json"
	"fmt"
	"os"
)

const maxJSONObjectFileBytes = 256 * 1024

// ParseJSONObject reads a flag value that carries a JSON object, either inline
// or as the path to a file holding it. subject names the flag in errors, so
// they read as "payload must be a JSON object" or "input must be a JSON
// object".
func ParseJSONObject(subject, value string) (map[string]any, error) {
	data := []byte(value)
	if info, err := os.Stat(value); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s file %q must be a regular file", subject, value)
		}
		if info.Size() > maxJSONObjectFileBytes {
			return nil, fmt.Errorf("%s file %q exceeds 256 KiB", subject, value)
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
	if object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", subject)
	}
	return object, nil
}
