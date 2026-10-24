package envfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertReplacesEveryCopyOfAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud.env")
	require.NoError(t, os.WriteFile(path, []byte("VOLCANO_ANON_KEY=ak-a\nSTRIPE_KEY=sk\nexport VOLCANO_ANON_KEY=ak-b\nexport\tVOLCANO_ANON_KEY=ak-c\nexportVOLCANO_ANON_KEY=other\n"), 0o600))

	require.NoError(t, Upsert(path, []Var{{Key: "VOLCANO_ANON_KEY", Value: "ak-new"}}, "# header"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "VOLCANO_ANON_KEY=ak-new\nSTRIPE_KEY=sk\nexportVOLCANO_ANON_KEY=other\n", string(data))
}

func TestUpsertStartsANewFileWithTheHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "volcano", "cloud.env")

	require.NoError(t, Upsert(path, []Var{{Key: "A", Value: "1"}}, "# header"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# header\nA=1\n", string(data))
}

func TestHasKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cloud.env")
	found, err := HasKey(path, "A")
	require.NoError(t, err)
	assert.False(t, found, "a missing file sets no key")

	require.NoError(t, os.WriteFile(path, []byte("# A=commented\nB=2\nexport A=1\n"), 0o600))
	found, err = HasKey(path, "A")
	require.NoError(t, err)
	assert.True(t, found)
	found, err = HasKey(path, "C")
	require.NoError(t, err)
	assert.False(t, found)
}
