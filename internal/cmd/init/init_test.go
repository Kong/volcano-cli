package initcmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpOutput(t *testing.T) {
	out, err := executeInitCommand(t, "--help")
	require.NoError(t, err)

	assert.Contains(t, out, "Create Volcano project scaffold")
	assert.Contains(t, out, "--force")
	assert.Contains(t, out, "--example")
	assert.Contains(t, out, "volcano init nextjs")
	assert.Contains(t, out, "volcano init nextjs --example notes")
	assert.Contains(t, out, "volcano init js --example hello-world")
}

func TestRejectsUnknownTemplate(t *testing.T) {
	_, err := executeInitCommand(t, "unknown-template")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown-template")
}

func TestRejectsExampleWithoutTemplate(t *testing.T) {
	_, err := executeInitCommand(t, "--example", "hello-world")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--example requires a template")
}

func TestCreatesScaffoldAndPrintsCreatedFiles(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeInitCommand(t)
	require.NoError(t, err)

	assert.Contains(t, out, "Volcano project initialized.")
	assert.Contains(t, out, "Created:")
	assert.Contains(t, out, "volcano/functions/hello.js")
	assert.Contains(t, out, "volcano config deploy")
	assert.Contains(t, out, "volcano functions deploy --all")
	assert.Contains(t, out, "volcano cloud config deploy")
	assert.Contains(t, out, "volcano cloud functions deploy --all")
	assert.FileExists(t, filepath.Join("volcano", "functions", "hello.js"))
}

func TestCreatesNextJSStarter(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeInitCommand(t, "nextjs")
	require.NoError(t, err)

	assert.Contains(t, out, "package.json")
	assert.Contains(t, out, "npm run dev")
	assert.FileExists(t, filepath.Join("web", "app", "page.js"))
	assert.FileExists(t, filepath.Join("web", "app", "globals.css"))
	assert.FileExists(t, ".gitignore")
	assert.Contains(t, readFile(t, ".gitignore"), "node_modules")
	assert.NoFileExists(t, filepath.Join("volcano", "functions", "notes-summary.js"))
	assert.NoFileExists(t, filepath.Join("volcano", "volcano-config.yaml"))
	assert.NotContains(t, out, "volcano config deploy")
	assert.NotContains(t, out, "volcano cloud config deploy")
}

func TestCreatesNextJSNotesExample(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeInitCommand(t, "nextjs", "--example", "notes")
	require.NoError(t, err)

	assert.Contains(t, out, "volcano/functions/notes-summary.js")
	assert.FileExists(t, filepath.Join("volcano", "migrations", "001_create_notes.sql"))
	assert.FileExists(t, filepath.Join("volcano", "functions", "notes-summary.js"))
	assert.FileExists(t, filepath.Join("web", "app", "dashboard", "page.js"))
	assert.FileExists(t, ".gitignore")
	assert.Contains(t, readFile(t, ".gitignore"), "node_modules")
	assert.NoFileExists(t, filepath.Join("volcano", "volcano-config.yaml"))
	assert.NotContains(t, out, "volcano config deploy")
	assert.NotContains(t, out, "volcano cloud config deploy")
	assert.Contains(t, readFile(t, filepath.Join("web", "README.md")), "volcano init nextjs --example notes")
	assert.Contains(t, readFile(t, filepath.Join("web", "app", "dashboard", "page.js")), `functions.invoke("notes-summary", { limit: 5 })`)
}

func TestRejectsLegacyNextJSDemoAlias(t *testing.T) {
	t.Chdir(t.TempDir())

	_, err := executeInitCommand(t, "nextjs-demo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown template")
}

func TestCreatesFunctionLanguageTemplates(t *testing.T) {
	tests := []struct {
		name     string
		template string
		manifest string
		function string
		contains string
	}{
		{name: "javascript", template: "javascript", manifest: filepath.Join("volcano", "volcano-config.yaml"), function: filepath.Join("volcano", "functions", "hello.js"), contains: "process.env.GREETING"},
		{name: "js alias", template: "js", manifest: filepath.Join("volcano", "volcano-config.yaml"), function: filepath.Join("volcano", "functions", "hello.js"), contains: "process.env.GREETING"},
		{name: "python", template: "python", manifest: "requirements.txt", function: filepath.Join("volcano", "functions", "hello.py"), contains: "ok"},
		{name: "ruby", template: "ruby", manifest: "Gemfile", function: filepath.Join("volcano", "functions", "hello.rb"), contains: "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := executeInitCommand(t, tt.template)
			require.NoError(t, err)
			assert.FileExists(t, tt.manifest)
			assert.FileExists(t, tt.function)
			assert.Contains(t, readFile(t, tt.function), tt.contains)
			if tt.template != "javascript" && tt.template != "js" {
				assert.NoFileExists(t, filepath.Join("volcano", "functions", "hello.js"))
				assert.NoFileExists(t, filepath.Join("volcano", "volcano-config.yaml"))
			}
		})
	}
}

func TestCreatesFunctionLanguageExamples(t *testing.T) {
	tests := []struct {
		template string
		function string
	}{
		{template: "js", function: filepath.Join("volcano", "functions", "hello.js")},
		{template: "python", function: filepath.Join("volcano", "functions", "hello.py")},
		{template: "ruby", function: filepath.Join("volcano", "functions", "hello.rb")},
	}

	for _, tt := range tests {
		t.Run(tt.template, func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := executeInitCommand(t, tt.template, "--example", "hello-world")
			require.NoError(t, err)
			assert.FileExists(t, tt.function)
			assert.Contains(t, readFile(t, tt.function), "Hello from Volcano")
		})
	}
}

func TestRerunPrintsUnchangedFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := executeInitCommand(t)
	require.NoError(t, err)

	out, err := executeInitCommand(t)
	require.NoError(t, err)

	assert.Contains(t, out, "Unchanged:")
	assert.Contains(t, out, "volcano/functions/hello.js")
	assert.NotContains(t, out, "Created:")
}

func TestConflictPrintsConflictingFilesWithoutPartialWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join("volcano", "functions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "functions", "hello.js"), []byte("custom\n"), 0o644))

	out, err := executeInitCommand(t)
	require.Error(t, err)

	assert.Contains(t, out, "Conflicts:")
	assert.Contains(t, out, "volcano/functions/hello.js")
	assert.Contains(t, out, "has different content")
	assert.Contains(t, out, "Re-run with --force")
	assert.NoFileExists(t, filepath.Join("volcano", "README.md"))
}

func TestPathTypeConflictDoesNotSuggestForce(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("volcano", []byte("binary\n"), 0o644))

	out, err := executeInitCommand(t)
	require.Error(t, err)

	assert.Contains(t, out, "volcano (exists and is not a directory)")
	assert.Contains(t, out, "Remove or rename incompatible paths")
	assert.NotContains(t, out, "Re-run with --force")
}

func TestForceOverwritesChangedManagedFile(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("volcano", "functions", "hello.js")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("custom\n"), 0o644))

	out, err := executeInitCommand(t, "--force")
	require.NoError(t, err)

	assert.Contains(t, out, "Overwritten:")
	assert.Contains(t, out, "volcano/functions/hello.js")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "process.env.GREETING")
	assert.NotContains(t, string(body), "custom")
}

func executeInitCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
