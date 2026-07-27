package runtime

import "testing"

// TestOpenURLRejectsNonHTTP verifies OpenURL refuses schemes other than
// http/https before shelling out to open/xdg-open/rundll32, so a malicious
// backend can't launch file://, custom-scheme handlers, or flag-like args.
func TestOpenURLRejectsNonHTTP(t *testing.T) {
	for _, rawURL := range []string{
		"file:///etc/passwd",
		"vscode://example",
		"javascript:alert(1)",
		"-flag",
		"",
		"://nohost",
	} {
		if err := OpenURL(rawURL); err == nil {
			t.Errorf("OpenURL(%q) = nil, want error", rawURL)
		}
	}
}
