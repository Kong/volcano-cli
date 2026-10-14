package initcmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/projectconfig"
)

func TestHelpOutput(t *testing.T) {
	out, err := executeInitCommand(t, "--help")
	require.NoError(t, err)

	assert.Contains(t, out, "Create Volcano project scaffold")
	assert.Contains(t, out, "--force")
	assert.Contains(t, out, "--example")
	assert.Contains(t, out, "volcano init nextjs")
	assert.Contains(t, out, "volcano init nextjs --example notes")
	assert.Contains(t, out, "volcano init javascript --example hello-world")
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
	assert.Contains(t, out, "volcano/README.md")
	assert.Contains(t, out, "2. Run: volcano deploy --with-migrations")
	assert.Contains(t, out, "Put the cloud project's variables in volcano/cloud.env")
	assert.Contains(t, out, "Run: volcano cloud deploy --env-file volcano/cloud.env\n")
	assert.NotContains(t, out, "Open the frontend URL")
	assert.FileExists(t, filepath.Join("volcano", "README.md"))
	assert.NoFileExists(t, filepath.Join("volcano", "functions", "hello.js"))
	assert.NoFileExists(t, filepath.Join("volcano", "volcano-config.yaml"))
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
	assert.Contains(t, out, "3. Open the frontend URL the deploy prints")
	assertStarterLayoutListsWeb(t)
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
	assert.FileExists(t, filepath.Join("volcano", "volcano-config.yaml"))
	assert.Contains(t, out, "2. Run: volcano deploy --with-migrations")
	assert.Contains(t, out, "Run: volcano cloud deploy --env-file volcano/cloud.env --with-migrations -d <database>")
	assert.Contains(t, readFile(t, filepath.Join("web", "README.md")), "volcano init nextjs --example notes")
	assert.Contains(t, readFile(t, filepath.Join("web", "README.md")), "volcano deploy --with-migrations")
	assertStarterLayoutListsWeb(t)

	// The dashboard invokes notes-summary as the signed-in user, which new
	// functions refuse until they are authenticated.
	manifest, _, err := projectconfig.Load(filepath.Join("volcano", "volcano-config.yaml"))
	require.NoError(t, err)
	require.NotNil(t, manifest.Functions)
	require.Len(t, *manifest.Functions, 1)
	assert.Equal(t, "notes-summary", (*manifest.Functions)[0].Name)
	assertStarterFunctionVisibility(t, "notes-summary", "authenticated")
	assert.Contains(t, readFile(t, filepath.Join("web", "app", "dashboard", "page.js")), `functions.invoke("notes-summary", { limit: 5 })`)

	// The frontend archive is the project root, so the build must select its
	// variables rather than receive the service key too.
	require.NotNil(t, manifest.Frontends)
	require.Len(t, *manifest.Frontends, 1)
	web := (*manifest.Frontends)[0]
	require.NotNil(t, web.VariableScope)
	assert.Equal(t, "scoped", *web.VariableScope)
	require.NotNil(t, web.Variables)
	assert.Equal(t, []string{"NEXT_PUBLIC_VOLCANO_API_URL", "NEXT_PUBLIC_VOLCANO_ANON_KEY", "NEXT_PUBLIC_VOLCANO_DATABASE"}, *web.Variables)
	// Frontend builds drop VOLCANO_* names, so the env file has to carry
	// every name the frontend selects.
	env := readFile(t, filepath.Join("volcano", "volcano.env"))
	for _, name := range *web.Variables {
		assert.Contains(t, env, name+"=")
	}
	assert.Contains(t, readFile(t, ".gitignore"), "volcano/*.env")

	// The upload is the whole project, and the env files hold keys.
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "cloud.env"), []byte("VOLCANO_ANON_KEY=ak-cloud\n"), 0o600))
	pkg, err := clifrontend.PackageDirectory(".", clifrontend.PackageOptions{AppRoot: "web"})
	require.NoError(t, err)
	entries := archiveEntries(t, pkg.Archive)
	assert.Contains(t, entries, "web/next.config.js")
	assert.Contains(t, entries, "volcano/_shared/volcano-client.js")
	assert.NotContains(t, entries, "volcano/volcano.env")
	assert.NotContains(t, entries, "volcano/cloud.env")
	assert.NotContains(t, entries, "web/.env.local")
}

func archiveEntries(t *testing.T, archive []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names
		}
		require.NoError(t, err)
		names = append(names, strings.TrimPrefix(header.Name, "./"))
	}
}

// assertStarterLayoutListsWeb checks that volcano deploy finds the starter's
// app as the web frontend and can package it.
func assertStarterLayoutListsWeb(t *testing.T) {
	t.Helper()
	layout, found, err := clifrontend.ReadSourceLayout(".")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, layout.Frontends, 1)
	web := layout.Frontends[0]
	assert.Equal(t, "web", web.Name)
	assert.Empty(t, web.Dir())
	assert.Equal(t, "web", web.AppRoot)
	buildable, err := clifrontend.HasBuildPackage(".", web.AppRoot)
	require.NoError(t, err)
	assert.True(t, buildable)
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
			if tt.template == "javascript" || tt.template == "js" {
				assertStarterFunctionVisibility(t, "hello", "public")
			} else {
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
			if tt.template == "js" {
				assertStarterFunctionVisibility(t, "hello", "public")
			}
		})
	}
}

// assertStarterFunctionVisibility checks the level a starter's manifest gives a
// function, spelled the current way rather than with the deprecated public.
func assertStarterFunctionVisibility(t *testing.T, name, visibility string) {
	t.Helper()
	manifest, _, err := projectconfig.Load(filepath.Join("volcano", "volcano-config.yaml"))
	require.NoError(t, err)
	require.NotNil(t, manifest.Functions)
	for _, fn := range *manifest.Functions {
		if fn.Name == name {
			require.NotNil(t, fn.Visibility)
			assert.Equal(t, visibility, *fn.Visibility)
			assert.Nil(t, fn.Public)
			return
		}
	}
	t.Fatalf("starter manifest declares no function %q", name)
}

func TestRerunPrintsUnchangedFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	_, err := executeInitCommand(t)
	require.NoError(t, err)

	out, err := executeInitCommand(t)
	require.NoError(t, err)

	assert.Contains(t, out, "Unchanged:")
	assert.Contains(t, out, "volcano/README.md")
	assert.NotContains(t, out, "Created:")
}

func TestConflictPrintsConflictingFilesWithoutPartialWrites(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "README.md"), []byte("custom\n"), 0o644))

	out, err := executeInitCommand(t)
	require.Error(t, err)

	assert.Contains(t, out, "Conflicts:")
	assert.Contains(t, out, "volcano/README.md")
	assert.Contains(t, out, "has different content")
	assert.Contains(t, out, "Re-run with --force")
	assert.NoFileExists(t, filepath.Join("volcano", "volcano.env"))
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
	path := filepath.Join("volcano", "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("custom\n"), 0o644))

	out, err := executeInitCommand(t, "--force")
	require.NoError(t, err)

	assert.Contains(t, out, "Overwritten:")
	assert.Contains(t, out, "volcano/README.md")
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Volcano")
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
