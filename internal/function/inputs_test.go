package function

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A package records each project file its archive holds, and names every
// file it reads or would read, so a deploy can compare them with the commit.
func TestPackageSourceRecordsItsInputs(t *testing.T) {
	dir := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(dir, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	write("package.json", `{"name":"root"}`)
	write("volcano/functions/hello/index.js", "exports.handler = 1;")
	write("volcano/functions/hello/package.json", `{"name":"hello"}`)
	write("volcano/functions/hello/node_modules/pkg/index.js", "skip")
	write("volcano/functions/_runtime/defaults.json", `{"ok":true}`)
	write("volcano/lib/_helpers/format.js", "module.exports = {};")
	write("volcano/functions/other.js", "exports.handler = 2;")

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
	// The root package.json is not read: the function's own takes its place.
	assert.ElementsMatch(t, []string{
		"volcano/functions/hello/index.js",
		"volcano/functions/hello/package.json",
		"volcano/functions/_runtime/defaults.json",
		"volcano/lib/_helpers/format.js",
	}, read)
	assert.Equal(t, dir, pkg.Inputs.Dir)

	feeds := pkg.Inputs.Feeds
	require.NotNil(t, feeds)
	for _, path := range []string{
		"volcano/functions/hello/index.js",
		"volcano/functions/hello/deleted.js",
		"volcano/functions/hello/node_modules/pkg/index.js",
		"volcano/functions/hello/pnpm-lock.yaml",
		"volcano/functions/package.json",
		"volcano/package.json",
		"package.json",
		"pnpm-lock.yaml",
		"volcano/functions/_runtime/defaults.json",
		"volcano/functions/_deleted.js",
		"volcano/lib/_helpers/format.js",
		"volcano/_shared.js",
	} {
		assert.True(t, feeds(path), path)
	}
	for _, path := range []string{
		"volcano/functions/other.js",
		"volcano/functions/hellothere/index.js",
		"volcano/functions/other/_private.js",
		"volcano/lib/format.js",
		"src/index.js",
		"README.md",
	} {
		assert.False(t, feeds(path), path)
	}
}

// A single-file function's own file is its input, and no other in the
// functions directory is.
func TestInputPathsOfASingleFileFunction(t *testing.T) {
	dir := t.TempDir()
	feeds := inputPaths(SourceInfo{
		Path:    filepath.Join(dir, "volcano", "functions", "hello.js"),
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"package.json"}),
	}, dir)
	require.NotNil(t, feeds)
	assert.True(t, feeds("volcano/functions/hello.js"))
	assert.True(t, feeds("volcano/functions/package.json"))
	assert.True(t, feeds("package.json"))
	assert.False(t, feeds("volcano/functions/hello.ts"))
	assert.False(t, feeds("volcano/functions/other.js"))

	assert.Nil(t, inputPaths(SourceInfo{Path: filepath.Join(t.TempDir(), "hello.js")}, dir), "a function outside the project")
}
