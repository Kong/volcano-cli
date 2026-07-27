package projectinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCreatesScaffold(t *testing.T) {
	dir := t.TempDir()

	result, err := run(dir, "", false)
	require.NoError(t, err)

	for _, path := range []string{
		"volcano",
		filepath.Join("volcano", "migrations"),
		filepath.Join("volcano", ".gitignore"),
		filepath.Join("volcano", "volcano.env"),
		filepath.Join("volcano", "volcano.env.example"),
		filepath.Join("volcano", "migrations", "README.md"),
		filepath.Join("volcano", "README.md"),
	} {
		assert.Contains(t, result.Created(), path)
		assertPathExists(t, filepath.Join(dir, path))
	}

	assert.Empty(t, result.Unchanged())
	assert.Empty(t, result.Overwritten())
	assert.NoFileExists(t, filepath.Join(dir, "volcano.env"))
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "functions", "hello.js"))
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "volcano-config.yaml"))

	ignore := readProjectFile(t, dir, filepath.Join("volcano", ".gitignore"))
	assert.Contains(t, ignore, "volcano.env")

	readme := readProjectFile(t, dir, filepath.Join("volcano", "README.md"))
	assert.Contains(t, readme, "If this project includes volcano/functions:\n\n    volcano functions deploy --all")
	assert.Contains(t, readme, "If this project includes volcano/functions:\n\n    volcano cloud functions deploy --all")
	assert.Contains(t, readme, "If this project includes volcano/volcano-config.yaml:\n\n    volcano config deploy")
	assert.Contains(t, readme, "If this project includes volcano/volcano-config.yaml:\n\n    volcano cloud config deploy")
	assert.Equal(t, 1, strings.Count(readme, "volcano functions deploy --all"))
	assert.Equal(t, 1, strings.Count(readme, "volcano cloud functions deploy --all"))
	assert.Equal(t, 1, strings.Count(readme, "volcano config deploy"))
	assert.Equal(t, 1, strings.Count(readme, "volcano cloud config deploy"))
	assert.NotContains(t, readme, "configuration, functions, migrations")
}

func TestRunRejectsInvalidStarterNames(t *testing.T) {
	tests := []string{
		"../starters",
		"nextjs/../base",
		"nextjs\\notes",
		"nextjs..notes",
		"nextjs_notes",
		"-nextjs",
		"nextjs-",
	}

	for _, starterName := range tests {
		t.Run(starterName, func(t *testing.T) {
			_, err := run(t.TempDir(), starterName, false)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid starter name")
		})
	}
}

func TestRunTemplateCreatesNextJSStarter(t *testing.T) {
	dir := t.TempDir()

	result, err := run(dir, "nextjs", false)
	require.NoError(t, err)

	assert.Contains(t, result.Created(), "package.json")
	assert.Contains(t, result.Created(), ".gitignore")
	assert.Contains(t, result.Created(), filepath.Join("web", "app", "page.js"))
	assert.Equal(t, 1, countPath(result.Created(), "volcano"))
	assertPathExists(t, filepath.Join(dir, "web", "app", "page.js"))
	assert.Contains(t, readProjectFile(t, dir, ".gitignore"), "node_modules")
	assert.Contains(t, readProjectFile(t, dir, filepath.Join("web", ".gitignore")), ".env*.local")
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "functions", "notes-summary.js"))
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "volcano-config.yaml"))
}

func TestRunTemplateCreatesNextJSNotesExample(t *testing.T) {
	dir := t.TempDir()

	result, err := run(dir, "nextjs-notes", false)
	require.NoError(t, err)

	assert.Contains(t, result.Created(), filepath.Join("volcano", "functions", "notes-summary.js"))
	assert.Contains(t, result.Created(), filepath.Join("volcano", "migrations", "001_create_notes.sql"))
	assert.Equal(t, 1, countPath(result.Created(), "volcano"))
	assert.Equal(t, 1, countPath(result.Created(), filepath.Join("volcano", "migrations")))
	assertPathExists(t, filepath.Join(dir, "web", "app", "dashboard", "page.js"))
	assert.Contains(t, readProjectFile(t, dir, ".gitignore"), "node_modules")
	assert.Contains(t, readProjectFile(t, dir, filepath.Join("web", ".gitignore")), ".env*.local")
	assert.Contains(t, readProjectFile(t, dir, filepath.Join("web", "app", "dashboard", "page.js")), `functions.invoke("notes-summary", { limit: 5 })`)
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "volcano-config.yaml"))
}

func TestRunTemplateCreatesLanguageStartersAndDemos(t *testing.T) {
	tests := []struct {
		name     string
		starter  string
		manifest string
		function string
		contains string
	}{
		{name: "javascript", starter: "javascript", manifest: filepath.Join("volcano", "volcano-config.yaml"), function: filepath.Join("volcano", "functions", "hello.js"), contains: "process.env.GREETING"},
		{name: "python", starter: "python", manifest: "requirements.txt", function: filepath.Join("volcano", "functions", "hello.py"), contains: "ok"},
		{name: "python hello-world", starter: "python-hello-world", manifest: "requirements.txt", function: filepath.Join("volcano", "functions", "hello.py"), contains: "Hello from Volcano Python"},
		{name: "ruby", starter: "ruby", manifest: "Gemfile", function: filepath.Join("volcano", "functions", "hello.rb"), contains: "ok"},
		{name: "ruby hello-world", starter: "ruby-hello-world", manifest: "Gemfile", function: filepath.Join("volcano", "functions", "hello.rb"), contains: "Hello from Volcano Ruby"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			_, err := run(dir, tt.starter, false)
			require.NoError(t, err)
			assertPathExists(t, filepath.Join(dir, tt.manifest))
			assertPathExists(t, filepath.Join(dir, tt.function))
			assert.Contains(t, readProjectFile(t, dir, tt.function), tt.contains)
		})
	}
}

func TestRunIsIdempotentForExactScaffold(t *testing.T) {
	dir := t.TempDir()
	_, err := run(dir, "", false)
	require.NoError(t, err)

	result, err := run(dir, "", false)
	require.NoError(t, err)

	assert.Empty(t, result.Created())
	assert.Empty(t, result.Overwritten())
	assert.Contains(t, result.Unchanged(), filepath.Join("volcano", "volcano.env"))
	assert.Contains(t, result.Unchanged(), filepath.Join("volcano", "README.md"))
}

func TestRunTemplateRerunDoesNotDuplicateUnchangedDirs(t *testing.T) {
	dir := t.TempDir()
	_, err := run(dir, "nextjs-notes", false)
	require.NoError(t, err)

	result, err := run(dir, "nextjs-notes", false)
	require.NoError(t, err)

	assert.Equal(t, 1, countPath(result.Unchanged(), "volcano"))
	assert.Equal(t, 1, countPath(result.Unchanged(), filepath.Join("volcano", "migrations")))
}

func TestRunConflictsDoNotWritePartialScaffold(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "README.md"), []byte("custom\n"), 0o644))

	result, err := run(dir, "", false)
	require.Nil(t, result)
	var conflictErr *conflictError
	require.ErrorAs(t, err, &conflictErr)
	require.Len(t, conflictErr.conflicts, 1)
	assert.Equal(t, filepath.Join("volcano", "README.md"), conflictErr.conflicts[0].Path)
	assert.Contains(t, conflictErr.conflicts[0].Reason, "different content")

	assert.NoFileExists(t, filepath.Join(dir, "volcano", "volcano.env"))
	assert.Equal(t, "custom\n", readProjectFile(t, dir, filepath.Join("volcano", "README.md")))
}

func TestRunForceOverwritesChangedManagedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "README.md"), []byte("custom\n"), 0o644))

	result, err := run(dir, "", true)
	require.NoError(t, err)

	assert.Contains(t, result.Overwritten(), filepath.Join("volcano", "README.md"))
	assert.NotContains(t, readProjectFile(t, dir, filepath.Join("volcano", "README.md")), "custom")
	assert.Contains(t, readProjectFile(t, dir, filepath.Join("volcano", "README.md")), "Volcano")
}

func TestRunRejectsFileWhereDirectoryIsNeeded(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano"), []byte("not a dir\n"), 0o644))

	_, err := run(dir, "", false)

	var conflictErr *conflictError
	require.ErrorAs(t, err, &conflictErr)
	require.Len(t, conflictErr.conflicts, 1)
	assert.Equal(t, "volcano", conflictErr.conflicts[0].Path)
	assert.Contains(t, conflictErr.conflicts[0].Reason, "not a directory")
}

func TestRunRespectsLegacyRootEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano.env"), []byte("ROOT_ENV=true\n"), 0o644))

	result, err := run(dir, "", false)
	require.NoError(t, err)

	assert.Contains(t, result.Unchanged(), "volcano.env")
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "volcano.env"))
	assertPathExists(t, filepath.Join(dir, "volcano", "volcano.env.example"))
}

func TestRunRejectsAmbiguousRootAndNestedEnv(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano.env"), []byte("ROOT=true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "volcano.env"), []byte("NESTED=true\n"), 0o644))

	_, err := run(dir, "", false)

	require.Error(t, err)
	var conflictErr *conflictError
	assert.NotErrorAs(t, err, &conflictErr)
	assert.Contains(t, err.Error(), "found multiple volcano.env files: volcano/volcano.env, volcano.env")
	assert.Contains(t, err.Error(), "please keep only one volcano.env file")
	assert.NoFileExists(t, filepath.Join(dir, "volcano", "README.md"))
}

func readProjectFile(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	require.NoError(t, err)
	return string(data)
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.NoError(t, err)
}

func countPath(paths []string, wanted string) int {
	count := 0
	for _, path := range paths {
		if path == wanted {
			count++
		}
	}
	return count
}
