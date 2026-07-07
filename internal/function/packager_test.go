package function

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient/common"
)

func TestPackageSourceRenamesSingleFilesToStandardEntrypoints(t *testing.T) {
	for _, tc := range []struct {
		name       string
		runtime    common.FunctionRuntimeOption
		filename   string
		wantEntry  string
		sourceCode string
	}{
		{name: "node", runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil), filename: "hello.js", wantEntry: "index.js", sourceCode: "exports.handler = async () => ({ statusCode: 200 });"},
		{name: "python", runtime: testRuntimeOption("python3.12", "python", true, nil, "main.py", "handler", nil), filename: "hello.py", wantEntry: "main.py", sourceCode: "def handler(event, context): return {'statusCode': 200}"},
		{name: "ruby", runtime: testRuntimeOption("ruby3.4", "ruby", true, nil, "main.rb", "handler", nil), filename: "hello.rb", wantEntry: "main.rb", sourceCode: "def handler(event:, context:) = { statusCode: 200 }"},
		{name: "api metadata", runtime: testRuntimeOption("custom-jsx", "custom", true, nil, "server.jsx", "serve", nil), filename: "hello.jsx", wantEntry: "server.jsx", sourceCode: "export const serve = async () => ({ statusCode: 200 });"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			functionsDir := filepath.Join(dir, "volcano", "functions")
			require.NoError(t, os.MkdirAll(functionsDir, 0o755))
			path := filepath.Join(functionsDir, tc.filename)
			require.NoError(t, os.WriteFile(path, []byte(tc.sourceCode), 0o644))

			pkg, err := PackageSource(SourceInfo{
				Path:    path,
				Name:    "hello",
				Runtime: tc.runtime,
			}, dir)
			require.NoError(t, err)

			assert.Equal(t, "hello", pkg.Name)
			assert.Equal(t, tc.runtime.Name, pkg.Runtime)
			assert.Equal(t, tc.runtime.Deployment.Handler, pkg.Handler)
			names := packageArchiveNames(t, pkg.ArchiveData)
			assert.Contains(t, names, tc.wantEntry)
			assert.NotContains(t, names, tc.filename)
		})
	}
}

func TestPackageSourceIncludesManifestsAndSharedLibraries(t *testing.T) {
	dir := t.TempDir()
	functionDir := filepath.Join(dir, "volcano", "functions", "hello")
	require.NoError(t, os.MkdirAll(filepath.Join(functionDir, "node_modules", "pkg"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano", "lib", "_helpers"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano", "functions", "_runtime", "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"root"}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "index.js"), []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "ignored.txt"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "node_modules", "pkg", "index.js"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "lib", "_helpers", "format.js"), []byte("module.exports = {};"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "functions", "_runtime", "config", "defaults.json"), []byte(`{"ok":true}`), 0o644))

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(functionDir, "index.js"),
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"package.json"}),
		IsDir:   true,
	}, dir)
	require.NoError(t, err)

	names := packageArchiveNames(t, pkg.ArchiveData)
	assert.Contains(t, names, "index.js")
	assert.Contains(t, names, "package.json")
	assert.Contains(t, names, "lib/_helpers/format.js")
	assert.Contains(t, names, "_runtime/config/defaults.json")
	assert.NotContains(t, names, "ignored.txt")
	assert.NotContains(t, names, "node_modules/pkg/index.js")
}

func TestPackageSourceIncludesDirectoryFunctionNamedLikeIgnoredSegment(t *testing.T) {
	dir := t.TempDir()
	functionDir := filepath.Join(dir, "volcano", "functions", "build")
	require.NoError(t, os.MkdirAll(functionDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "index.js"), []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "generated.js"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("volcano/functions/build/generated.js\n"), 0o644))

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(functionDir, "index.js"),
		Name:    "build",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil),
		IsDir:   true,
	}, dir)
	require.NoError(t, err)

	names := packageArchiveNames(t, pkg.ArchiveData)
	assert.Contains(t, names, "index.js")
	assert.NotContains(t, names, "generated.js")
}

func TestPackageSourceAppliesIgnoresToSharedFiles(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello.js"), []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "_shared.js"), []byte("module.exports = {};"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "_debug.log"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "_secret.js"), []byte("skip"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("volcano/_secret.js\n"), 0o644))

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(functionsDir, "hello.js"),
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil),
	}, dir)
	require.NoError(t, err)

	names := packageArchiveNames(t, pkg.ArchiveData)
	assert.Contains(t, names, "index.js")
	assert.Contains(t, names, "_shared.js")
	assert.NotContains(t, names, "_debug.log")
	assert.NotContains(t, names, "_secret.js")
}

func TestPackageSourceSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	functionDir := filepath.Join(dir, "volcano", "functions", "hello")
	require.NoError(t, os.MkdirAll(functionDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionDir, "index.js"), []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))
	outside := filepath.Join(dir, "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(functionDir, "secret-link.txt")))

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(functionDir, "index.js"),
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil),
		IsDir:   true,
	}, dir)
	require.NoError(t, err)

	assert.NotContains(t, packageArchiveNames(t, pkg.ArchiveData), "secret-link.txt")
}

func TestPackageSourceRejectsSymlinkSourceFile(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	outside := filepath.Join(dir, "secret.js")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o644))
	sourcePath := filepath.Join(functionsDir, "hello.js")
	if err := os.Symlink(outside, sourcePath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := PackageSource(SourceInfo{
		Path:    sourcePath,
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil),
	}, dir)
	require.ErrorContains(t, err, "not a regular file")
}

func TestPackageSourceSkipsSymlinkManifestsAndSharedFiles(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello.js"), []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "_shared.js"), []byte("module.exports = {};"), 0o644))
	outsideManifest := filepath.Join(dir, "outside-package.json")
	outsideShared := filepath.Join(dir, "outside-shared.js")
	require.NoError(t, os.WriteFile(outsideManifest, []byte(`{"name":"secret"}`), 0o644))
	require.NoError(t, os.WriteFile(outsideShared, []byte("secret"), 0o644))
	if err := os.Symlink(outsideManifest, filepath.Join(dir, "package.json")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(outsideShared, filepath.Join(dir, "volcano", "_secret.js")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	pkg, err := PackageSource(SourceInfo{
		Path:    filepath.Join(functionsDir, "hello.js"),
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"package.json"}),
	}, dir)
	require.NoError(t, err)

	names := packageArchiveNames(t, pkg.ArchiveData)
	assert.Contains(t, names, "index.js")
	assert.Contains(t, names, "_shared.js")
	assert.NotContains(t, names, "package.json")
	assert.NotContains(t, names, "_secret.js")
}

func TestPackageSourceRejectsUnsafeRuntimeArchivePaths(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	path := filepath.Join(functionsDir, "hello.js")
	require.NoError(t, os.WriteFile(path, []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))

	_, err := PackageSource(SourceInfo{
		Path:    path,
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "../index.js", "handler", nil),
	}, dir)
	require.ErrorContains(t, err, "unsafe function entrypoint")

	_, err = PackageSource(SourceInfo{
		Path:    path,
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", []string{"../package.json"}),
	}, dir)
	require.ErrorContains(t, err, "unsafe dependency manifest")
}

func packageArchiveNames(t *testing.T, archiveData []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archiveData))
	require.NoError(t, err)
	defer gz.Close()
	tr := tar.NewReader(gz)

	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.FileInfo().IsDir() {
			continue
		}
		names = append(names, header.Name)
	}
	return names
}
