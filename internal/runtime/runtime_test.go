package runtime

import "testing"

// TestValidateOpenURL verifies the scheme guard rejects everything but
// http/https, so a malicious backend can't launch file://, custom-scheme
// handlers, or flag-like args, while still accepting real browser URLs.
func TestValidateOpenURL(t *testing.T) {
	reject := []string{
		"file:///etc/passwd",
		"vscode://example",
		"javascript:alert(1)",
		"-flag",
		"",
		"://nohost",
	}
	for _, rawURL := range reject {
		if err := validateOpenURL(rawURL); err == nil {
			t.Errorf("validateOpenURL(%q) = nil, want error", rawURL)
		}
	}

	accept := []string{
		"http://localhost:8000/device?user_code=ABCD",
		"https://volcano.dev/device",
	}
	for _, rawURL := range accept {
		if err := validateOpenURL(rawURL); err != nil {
			t.Errorf("validateOpenURL(%q) = %v, want nil", rawURL, err)
		}
	}
}
