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
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
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
	return object, nil
}
