package config

import "testing"

func TestCompiledEnvironmentLabel(t *testing.T) {
	cases := []struct {
		apiURL string
		want   string
	}{
		{"https://api.volcano.dev", "production"},
		{"https://api.staging.volcano.dev", "staging"},
		{"http://localhost:54321", "custom"},
		{"https://api.example.com", "custom"},
		{"", "custom"},
	}
	orig := compiledDefaultAPIURL
	defer func() { compiledDefaultAPIURL = orig }()
	for _, c := range cases {
		compiledDefaultAPIURL = c.apiURL
		if got := CompiledEnvironmentLabel(); got != c.want {
			t.Errorf("CompiledEnvironmentLabel(%q) = %q, want %q", c.apiURL, got, c.want)
		}
		if CompiledDefaultAPIURL() != c.apiURL {
			t.Errorf("CompiledDefaultAPIURL() = %q, want %q", CompiledDefaultAPIURL(), c.apiURL)
		}
	}
}
