package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanMigrationsSortsTopLevelSQLFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "volcano", "migrations")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "002_second.sql"), []byte("-- second"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "001_first.SQL"), []byte("-- first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nested", "003_nested.sql"), []byte("-- nested"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore"), 0o644))

	files, err := scanMigrations(dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"001_first.SQL", "002_second.sql"}, migrationBaseNames(files))
}

func TestScanMigrationsMissingDirectoryReturnsEmpty(t *testing.T) {
	files, err := scanMigrations(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestNormalizeTargetMigration(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   string
	}{
		{target: "001_create_users", want: "001_create_users"},
		{target: "001_create_users.sql", want: "001_create_users"},
		{target: filepath.Join("volcano", "migrations", "001_create_users.sql"), want: "001_create_users"},
		{target: filepath.Join("migrations", "001_create_users.SQL"), want: "001_create_users"},
	} {
		assert.Equal(t, tc.want, normalizeTargetMigration(tc.target))
	}
}

func migrationBaseNames(files []string) []string {
	values := make([]string, 0, len(files))
	for _, file := range files {
		values = append(values, filepath.Base(file))
	}
	return values
}
