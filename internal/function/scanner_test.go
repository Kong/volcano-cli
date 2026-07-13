package function

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func TestRuntimeCatalogFromOptions(t *testing.T) {
	catalog := RuntimeCatalogFromOptions([]apiclient.FunctionRuntimeOption{
		testRuntimeOption("nodejs24.x", "nodejs", true, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json"}),
		testRuntimeOption("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
		testRuntimeOption("ruby3.4", "ruby", true, []string{".rb"}, "main.rb", "handler", []string{"Gemfile", "Gemfile.lock"}),
		testRuntimeOption("nodejs22.x", "nodejs", false, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json"}),
	})

	runtime, ok := catalog.runtimeForFile("hello.py")
	require.True(t, ok)
	assert.Equal(t, testRuntimeOption("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}), runtime)

	runtime, ok = catalog.runtimeForFile("module.mjs")
	require.True(t, ok)
	assert.Equal(t, "nodejs24.x", runtime.Name)
}

func TestScanSourcesUsesRuntimeCatalogMetadata(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "directory-fn"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "api-fn"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "_utils"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "node.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "module.mjs"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "python.py"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "ruby.rb"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "custom.jsx"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "_shared.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "notes.txt"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "directory-fn", "index.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "api-fn", "server.jsx"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "_utils", "index.js"), []byte(""), 0o644))

	catalog := RuntimeCatalogFromOptions([]apiclient.FunctionRuntimeOption{
		testRuntimeOption("nodejs24.x", "nodejs", true, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json"}),
		testRuntimeOption("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
		testRuntimeOption("ruby3.4", "ruby", true, []string{".rb"}, "main.rb", "handler", []string{"Gemfile", "Gemfile.lock"}),
		testRuntimeOption("custom-jsx", "custom", true, []string{".jsx"}, "server.jsx", "serve", []string{"custom.lock"}),
	})

	sources, err := ScanSources(dir, catalog)
	require.NoError(t, err)

	var names []string
	runtimes := map[string]apiclient.FunctionRuntimeOption{}
	paths := map[string]string{}
	isDir := map[string]bool{}
	for _, source := range sources {
		names = append(names, source.Name)
		runtimes[source.Name] = source.Runtime
		paths[source.Name] = source.Path
		isDir[source.Name] = source.IsDir
	}
	assert.Equal(t, []string{"api-fn", "custom", "directory-fn", "module", "node", "python", "ruby"}, names)
	assert.Equal(t, "nodejs24.x", runtimes["module"].Name)
	assert.Equal(t, "nodejs24.x", runtimes["node"].Name)
	assert.Equal(t, "python3.12", runtimes["python"].Name)
	assert.Equal(t, "ruby3.4", runtimes["ruby"].Name)
	assert.Equal(t, testRuntimeOption("custom-jsx", "custom", true, []string{".jsx"}, "server.jsx", "serve", []string{"custom.lock"}), runtimes["custom"])
	assert.True(t, isDir["directory-fn"])
	assert.True(t, isDir["api-fn"])
	assert.Equal(t, filepath.Join(functionsDir, "api-fn", "server.jsx"), paths["api-fn"])
}

func TestScanSourcesRequiresDirectoryEntrypoints(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "node-dir"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "python-dir"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "python-index"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "ruby-dir"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "ruby-index"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "node-dir", "index.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "python-dir", "main.py"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "python-index", "index.py"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "ruby-dir", "main.rb"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "ruby-index", "index.rb"), []byte(""), 0o644))

	sources, err := ScanSources(dir, testRuntimeCatalog(t))
	require.NoError(t, err)

	runtimes := map[string]string{}
	for _, source := range sources {
		runtimes[source.Name] = source.Runtime.Name
	}
	assert.Equal(t, map[string]string{
		"node-dir":   "nodejs24.x",
		"python-dir": "python3.12",
		"ruby-dir":   "ruby3.4",
	}, runtimes)
}

func TestScanSourcesMissingDirectoryReturnsEmpty(t *testing.T) {
	sources, err := ScanSources(t.TempDir(), testRuntimeCatalog(t))
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestFindSharedLibrariesSkipsFunctionContents(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano", "lib", "_helpers"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano", "functions", "_runtime"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano", "functions", "hello", "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "_root.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "lib", "_helpers", "index.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "functions", "_runtime", "defaults.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "functions", "hello", "_private.js"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "functions", "hello", "nested", "_also-private.js"), []byte(""), 0o644))

	shared, err := FindSharedLibraries(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(shared))
	for _, lib := range shared {
		names = append(names, lib.Name)
	}
	assert.ElementsMatch(t, []string{"_root.js", "_runtime", "lib/_helpers"}, names)
}

func testRuntimeCatalog(t *testing.T) RuntimeCatalog {
	t.Helper()
	return RuntimeCatalogFromOptions([]apiclient.FunctionRuntimeOption{
		testRuntimeOption("nodejs24.x", "nodejs", true, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", ".yarnrc.yml"}),
		testRuntimeOption("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
		testRuntimeOption("ruby3.4", "ruby", true, []string{".rb"}, "main.rb", "handler", []string{"Gemfile", "Gemfile.lock"}),
	})
}

func testRuntimeOption(name, language string, isDefault bool, fileExtensions []string, entrypoint, handler string, dependencyManifests []string) apiclient.FunctionRuntimeOption {
	return apiclient.FunctionRuntimeOption{
		Name:     name,
		Language: language,
		Default:  isDefault,
		Deployment: apiclient.FunctionRuntimeDeployment{
			FileExtensions:      fileExtensions,
			Entrypoint:          entrypoint,
			Handler:             handler,
			DependencyManifests: dependencyManifests,
		},
	}
}
