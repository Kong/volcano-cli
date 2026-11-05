package function

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/ignore"
)

func writeInput(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}

// A package records each project file its archive holds, and names every
// file it reads or would read, by the rules it packaged with, so a deploy can
// compare them with the commit.
func TestPackageSourceRecordsItsInputs(t *testing.T) {
	dir := t.TempDir()
	for path, content := range map[string]string{
		".gitignore":                                        "secret.txt\n",
		"package.json":                                      `{"name":"root"}`,
		"volcano/functions/hello/index.js":                  "exports.handler = 1;",
		"volcano/functions/hello/package.json":              `{"name":"hello"}`,
		"volcano/functions/hello/secret.txt":                "skip",
		"volcano/functions/hello/node_modules/pkg/index.js": "skip",
		"volcano/functions/_runtime/defaults.json":          `{"ok":true}`,
		"volcano/lib/_helpers/format.js":                    "module.exports = {};",
		"volcano/_util.js":                                  "replaced",
		"volcano/functions/_util.js":                        "module.exports = 1;",
		"volcano/functions/other.js":                        "exports.handler = 2;",
	} {
		writeInput(t, dir, path, content)
	}

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(dir, "volcano", "functions", "hello", "index.js"),
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"package.json", "pnpm-lock.yaml"}),
		IsDir:   true,
	}, dir)
	require.NoError(t, err)

	read := make([]string, 0, len(pkg.Inputs.Files))
	for _, file := range pkg.Inputs.Files {
		rel, err := filepath.Rel(dir, file.Path)
		require.NoError(t, err)
		read = append(read, filepath.ToSlash(rel))
	}
	assert.ElementsMatch(t, []string{
		"volcano/functions/hello/index.js",
		"volcano/functions/hello/package.json",
		"volcano/functions/_runtime/defaults.json",
		"volcano/functions/_util.js",
		"volcano/lib/_helpers/format.js",
	}, read)
	assert.Equal(t, dir, pkg.Inputs.Dir)
	assert.Equal(t, []string{filepath.Join(dir, ".gitignore")}, pkg.Inputs.Paths)

	feeds := pkg.Inputs.Feeds
	require.NotNil(t, feeds)
	for _, path := range []string{
		"volcano/functions/hello/index.js",
		"volcano/functions/hello/deleted.js",
		"volcano/functions/hello/pnpm-lock.yaml",
		"volcano/functions/package.json",
		"volcano/package.json",
		"pnpm-lock.yaml",
		"volcano/functions/_runtime/defaults.json",
		"volcano/functions/_deleted.js",
		"volcano/functions/_util.js",
		"volcano/lib/_helpers/format.js",
		"volcano/_shared.js",
	} {
		assert.True(t, feeds(path), path)
	}
	for path, why := range map[string]string{
		"package.json": "the function's own package.json takes its archive path",
		"volcano/functions/hello/node_modules/pkg/index.js": "every upload leaves node_modules out",
		"volcano/functions/hello/debug.log":                 "every upload leaves *.log out",
		"volcano/functions/hello/secret.txt":                "the root .gitignore lists it",
		"volcano/_util.js":                                  "volcano/functions/_util.js replaces it",
		"volcano/functions/other.js":                        "another function",
		"volcano/functions/hellothere/index.js":             "another function",
		"volcano/functions/other/_private.js":               "inside another function",
		"volcano/lib/format.js":                             "not a shared library",
		"src/index.js":                                      "outside volcano",
	} {
		assert.False(t, feeds(path), "%s: %s", path, why)
	}
}

// A single-file function's own file is its input, and so is each manifest
// above it until one is there to take its archive path.
func TestInputPathsOfASingleFileFunction(t *testing.T) {
	dir := t.TempDir()
	matcher, err := ignore.NewProjectMatcher(dir, cloudIgnorePatterns()...)
	require.NoError(t, err)
	info := SourceInfo{
		Path:    filepath.Join(dir, "volcano", "functions", "hello.js"),
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"package.json"}),
	}

	feeds := inputPaths(info, dir, matcher, map[string]bool{"volcano/functions/hello.js": true}, nil)
	require.NotNil(t, feeds)
	assert.True(t, feeds("volcano/functions/hello.js"))
	assert.True(t, feeds("volcano/functions/package.json"))
	assert.True(t, feeds("package.json"))
	assert.False(t, feeds("volcano/functions/hello.ts"))
	assert.False(t, feeds("volcano/functions/other.js"))

	feeds = inputPaths(info, dir, matcher, map[string]bool{"volcano/functions/package.json": true}, nil)
	assert.False(t, feeds("package.json"), "volcano/functions/package.json takes its archive path")

	assert.Nil(t, inputPaths(SourceInfo{Path: filepath.Join(t.TempDir(), "hello.js")}, dir, matcher, nil, nil), "a function outside the project")
}
