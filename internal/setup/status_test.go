package setup

import (
	"path/filepath"
	"testing"
)

func TestPluginStatus(t *testing.T) {
	home := t.TempDir()
	seedClaudeVersions(t, home, "0.2.14", "0.2.16")
	mustMkdir(t, filepath.Join(home, ".cursor", "skills", "volcano-platform"))
	opts := Options{HomeDir: home, Getenv: emptyEnv, LookPath: noBins}

	cases := []struct {
		name        string
		wantOK      bool
		wantVersion string
	}{
		// claude is not on PATH: PluginStatus must not need the presence probe.
		{"claude-code", true, "0.2.14"},
		{"codex", false, ""},
		{"cursor", true, ""},
		{"opencode", false, ""},
		// Not a setup harness and no manual install yet.
		{"gemini-cli", false, ""},
	}
	for _, tc := range cases {
		ok, version := PluginStatus(opts, tc.name)
		if ok != tc.wantOK || version != tc.wantVersion {
			t.Errorf("PluginStatus(%q) = %v, %q; want %v, %q", tc.name, ok, version, tc.wantOK, tc.wantVersion)
		}
	}

	mustMkdir(t, filepath.Join(home, ".volcano", "skills", "volcano-sdk"))
	if ok, version := PluginStatus(opts, "gemini-cli"); !ok || version != "" {
		t.Errorf("PluginStatus(gemini-cli) with manual install = %v, %q; want true, \"\"", ok, version)
	}
}
