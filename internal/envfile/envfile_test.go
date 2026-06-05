package envfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaultsToNestedFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("NESTED=value\n"), 0o644))

	file, err := Load("")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("volcano", "volcano.env"), file.Path)
	assert.Equal(t, map[string]string{"NESTED": "value"}, file.Variables)
}

func TestLoadErrorsWhenBothDefaultsExist(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("NESTED=value\n"), 0o644))
	require.NoError(t, os.WriteFile("volcano.env", []byte("ROOT=value\n"), 0o644))

	_, err := Load("")
	require.ErrorContains(t, err, "found multiple volcano.env files")
}

func TestLoadUsesExplicitFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano.env", []byte("ROOT=value\n"), 0o644))

	file, err := Load("volcano.env")
	require.NoError(t, err)
	assert.Equal(t, "volcano.env", file.Path)
	assert.Equal(t, map[string]string{"ROOT": "value"}, file.Variables)
}

func TestLoadPathReadsExactFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("custom.env", []byte("CUSTOM=value\n"), 0o644))

	file, err := loadPath("custom.env")
	require.NoError(t, err)
	assert.Equal(t, "custom.env", file.Path)
	assert.Equal(t, map[string]string{"CUSTOM": "value"}, file.Variables)
}

func TestLoadFirstExistingUsesFirstPresentCandidate(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("second.env", []byte("SECOND=value\n"), 0o644))
	require.NoError(t, os.WriteFile("third.env", []byte("THIRD=value\n"), 0o644))

	file, found, err := loadFirstExisting("missing.env", "second.env", "third.env")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "second.env", file.Path)
	assert.Equal(t, map[string]string{"SECOND": "value"}, file.Variables)
}

func TestLoadFirstExistingReturnsNotFound(t *testing.T) {
	t.Chdir(t.TempDir())

	file, found, err := loadFirstExisting("missing.env", filepath.Join("also", "missing.env"))
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, file)
}

func TestLoadFirstEnvVars(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("second.env", []byte("ZED=last\nAPI=first\n"), 0o644))

	envs, err := LoadFirstEnvVars("missing.env", "second.env")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"API=first",
		"ZED=last",
	}, envs)
}

func TestResolvePathSurfacesStatErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano", []byte("not a directory"), 0o644))

	_, err := resolvePath(filepath.Join("volcano", "volcano.env"))
	require.ErrorContains(t, err, "failed to access volcano/volcano.env")
}

func TestLoadParsesEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano.env", []byte(`
	# comment
	API_KEY=secret
	QUOTED="hello world"
	SINGLE='hello again'
	export EXPORTED=value
	INLINE=hello # comment
	`), 0o644))

	file, err := Load("volcano.env")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"API_KEY":  "secret",
		"EXPORTED": "value",
		"INLINE":   "hello",
		"QUOTED":   "hello world",
		"SINGLE":   "hello again",
	}, file.Variables)
}

func TestFileEnvVarsReturnsSortedKeyValueEntries(t *testing.T) {
	file := &File{Variables: map[string]string{
		"ZED": "last",
		"API": "first",
		"MID": "middle",
	}}

	assert.Equal(t, []string{
		"API=first",
		"MID=middle",
		"ZED=last",
	}, file.envVars())
}

func TestLoadErrorsOnMalformedEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano.env", []byte("API_KEY\n"), 0o644))

	_, err := Load("volcano.env")
	require.ErrorContains(t, err, "failed to read volcano.env")
}

func TestLoadErrorsOnEmptyVariableName(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano.env", []byte("=secret\n"), 0o644))

	_, err := Load("volcano.env")
	require.ErrorContains(t, err, "empty variable name")
}
