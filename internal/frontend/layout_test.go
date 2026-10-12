package frontend

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSourceLayoutResolvesWorkspaces(t *testing.T) {
	layout, err := ParseSourceLayout([]byte(`{
  "version": 1,
  "frontends": [
    {"name": "web", "workspace": "", "app_root": "web"},
    {"name": "admin", "app_root": ""},
    {"name": "docs", "workspace": "apps", "app_root": "docs/site"}
  ]
}`))
	require.NoError(t, err)
	require.Len(t, layout.Frontends, 3)
	assert.Empty(t, layout.Frontends[0].Dir())
	assert.Equal(t, "web", layout.Frontends[0].AppRoot)
	assert.Equal(t, "frontends/admin", layout.Frontends[1].Dir())
	assert.Equal(t, "apps", layout.Frontends[2].Dir())
	assert.Equal(t, "docs/site", layout.Frontends[2].AppRoot)
}

func TestParseSourceLayoutRejectsWhatTheServerRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		want string
	}{
		{"malformed", `{`, "unexpected end of JSON input"},
		{"version", `{"version": 2, "frontends": []}`, "unsupported version 2"},
		{"uppercase name", `{"version": 1, "frontends": [{"name": "Web", "app_root": ""}]}`, `frontend "Web": name must be`},
		{"leading hyphen", `{"version": 1, "frontends": [{"name": "-web", "app_root": ""}]}`, "name must be"},
		{"escaping name", `{"version": 1, "frontends": [{"name": "../other", "app_root": ""}]}`, `frontend "../other": name must be`},
		{"escaping workspace", `{"version": 1, "frontends": [{"name": "web", "workspace": "../web", "app_root": ""}]}`, `workspace "../web"`},
		{"absolute workspace", `{"version": 1, "frontends": [{"name": "web", "workspace": "/apps/web", "app_root": ""}]}`, `workspace "/apps/web"`},
		{"unclean workspace", `{"version": 1, "frontends": [{"name": "web", "workspace": "apps/../web", "app_root": ""}]}`, `workspace "apps/../web"`},
		{"escaping app root", `{"version": 1, "frontends": [{"name": "web", "app_root": "../other"}]}`, `app_root "../other"`},
		{"absolute app root", `{"version": 1, "frontends": [{"name": "web", "app_root": "/web"}]}`, `app_root "/web"`},
		{"unclean app root", `{"version": 1, "frontends": [{"name": "web", "app_root": "apps/./web"}]}`, `app_root "apps/./web"`},
		{"padded app root", `{"version": 1, "frontends": [{"name": "web", "app_root": " web"}]}`, `app_root " web"`},
		{"backslash app root", `{"version": 1, "frontends": [{"name": "web", "app_root": "apps\\web"}]}`, `app_root "apps\\web"`},
		{"duplicate", `{"version": 1, "frontends": [{"name": "web", "app_root": ""}, {"name": "web", "app_root": "x"}]}`, `frontend "web" is listed twice`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSourceLayout([]byte(tc.data))
			require.ErrorContains(t, err, SourceLayoutPath)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestReadSourceLayoutReportsAMissingFile(t *testing.T) {
	root := t.TempDir()
	_, found, err := ReadSourceLayout(root)
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".volcano", "source-layout.json"),
		[]byte(`{"version": 1, "frontends": [{"name": "web", "workspace": "", "app_root": "web"}]}`), 0o644))
	layout, found, err := ReadSourceLayout(root)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "web", layout.Frontends[0].Name)
}
