package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeMigrations(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "volcano", "migrations")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func paths(files []MigrationFile) []string {
	out := make([]string, len(files))
	for i, file := range files {
		out[i] = file.Path
	}
	return out
}

func TestReadMigrationsFlatLayoutBelongsToAnyDatabase(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"002_second.sql":     "select 2;",
		"001_first.SQL":      "select 1;",
		"README.md":          "not a migration",
		".hidden.sql":        "skipped",
		"_draft.sql":         "skipped",
		"_archive/9_x.sql":   "skipped",
		".git/objects/x.sql": "skipped",
	})

	migrations, err := ReadMigrations(dir)
	require.NoError(t, err)
	assert.False(t, migrations.Nested())
	assert.Empty(t, migrations.Databases())
	assert.Equal(t, []string{"001_first.SQL", "002_second.sql"}, paths(migrations.For("app")))
	assert.Equal(t, []string{"001_first.SQL", "002_second.sql"}, paths(migrations.For("other")))
	assert.Equal(t, MigrationsDir, migrations.Location("app"))
}

func TestReadMigrationsNestedLayoutBelongsToEachDirectory(t *testing.T) {
	dir := writeMigrations(t, map[string]string{
		"main_db/002_b.sql":   "select 2;",
		"main_db/001_a.sql":   "select 1;",
		"analytics/001_e.sql": "select 3;",
		"_shared/x.sql":       "skipped",
	})

	migrations, err := ReadMigrations(dir)
	require.NoError(t, err)
	assert.True(t, migrations.Nested())
	assert.Equal(t, []string{"analytics", "main_db"}, migrations.Databases())
	files := migrations.For("main_db")
	assert.Equal(t, []string{"main_db/001_a.sql", "main_db/002_b.sql"}, paths(files))
	assert.Equal(t, "001_a.sql", files[0].Name())
	assert.Empty(t, migrations.For("app"))
	assert.Equal(t, MigrationsDir+"/main_db", migrations.Location("main_db"))
}

func TestReadMigrationsRefusesWhatGitDeployRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name:  "mixed layout",
			files: map[string]string{"001_top.sql": "select 1;", "app/001_a.sql": "select 1;"},
			want:  "volcano/migrations mixes top-level files, such as 001_top.sql, with database directories, such as app/",
		},
		{
			name:  "nested too deeply",
			files: map[string]string{"app/v1/001_a.sql": "select 1;"},
			want:  "app/v1/001_a.sql is nested too deeply; place migrations in volcano/migrations or one directory below it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadMigrations(writeMigrations(t, tc.files))
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestReadMigrationsRefusesASymlink(t *testing.T) {
	dir := writeMigrations(t, map[string]string{"001_a.sql": "select 1;"})
	require.NoError(t, os.Symlink(filepath.Join(dir, "001_a.sql"), filepath.Join(dir, "002_link.sql")))

	_, err := ReadMigrations(dir)
	require.ErrorContains(t, err, "002_link.sql is a symlink, which Git deploy does not follow")
}

func TestReadMigrationsWithoutTheDirectoryIsEmpty(t *testing.T) {
	migrations, err := ReadMigrations(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.True(t, migrations.Empty())
	assert.Empty(t, migrations.For("app"))
}

// The checksum is the API's: a byte order mark and CRLF line endings do not
// change it, so checkouts on different platforms agree.
func TestMigrationChecksumIgnoresBOMAndCRLF(t *testing.T) {
	// The digest of "test", as the API documents it.
	assert.Equal(t, "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", MigrationFile{SQL: "test"}.Checksum())
	assert.Equal(t, MigrationFile{SQL: "test"}.Checksum(), MigrationFile{SQL: "\uFEFFtest"}.Checksum())

	plain := MigrationFile{SQL: "CREATE TABLE notes (id int);\nSELECT 1;\n"}
	assert.Equal(t, plain.Checksum(), MigrationFile{SQL: "\uFEFFCREATE TABLE notes (id int);\r\nSELECT 1;\r\n"}.Checksum())
	assert.NotEqual(t, plain.Checksum(), MigrationFile{SQL: "CREATE TABLE notes (id int);\nSELECT 2;\n"}.Checksum())
}
