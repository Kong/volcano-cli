package function

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func TestScanSourcesFindsTypeScriptFunctions(t *testing.T) {
	for _, tc := range []struct {
		name string
		// files are written under volcano/functions.
		files []string
		// want maps each discovered function to its entry, relative to
		// volcano/functions, and is empty when none is found.
		want map[string]string
		// wantRuntime is every discovered function's runtime when set.
		wantRuntime string
	}{
		{name: "single file", files: []string{"hello.ts"}, want: map[string]string{"hello": "hello.ts"}, wantRuntime: "nodejs24.x"},
		{name: "es module file", files: []string{"hello.mts"}, want: map[string]string{"hello": "hello.mts"}},
		{name: "commonjs file", files: []string{"hello.cts"}, want: map[string]string{"hello": "hello.cts"}},
		{name: "declaration file holds no code", files: []string{"types.d.ts", "types.d.mts"}, want: map[string]string{}},
		{name: "jsx file", files: []string{"page.tsx"}, want: map[string]string{"page": "page.tsx"}, wantRuntime: "nodejs24.x"},
		{name: "jsx directory index", files: []string{"page/index.tsx", "page/view.tsx", "page/tsconfig.json"}, want: map[string]string{"page": "page/index.tsx"}, wantRuntime: "nodejs24.x"},
		{name: "javascript file wins over jsx file", files: []string{"page.js", "page.tsx"}, want: map[string]string{"page": "page.js"}},
		{name: "jsx directory wins over typescript file", files: []string{"page.ts", "page/index.tsx"}, want: map[string]string{"page": "page/index.tsx"}},
		{name: "directory javascript index wins over jsx", files: []string{"page/index.js", "page/index.tsx"}, want: map[string]string{"page": "page/index.js"}},
		{name: "directory python entrypoint wins over jsx", files: []string{"page/index.tsx", "page/main.py"}, want: map[string]string{"page": "page/main.py"}, wantRuntime: "python3.12"},
		{name: "directory jsx needs an index at its top", files: []string{"page/src/index.tsx", "page/view.tsx"}, want: map[string]string{}},
		{name: "shared code stays shared", files: []string{"_lib.ts", "_lib/index.ts"}, want: map[string]string{}},
		{name: "javascript file wins", files: []string{"hello.js", "hello.ts"}, want: map[string]string{"hello": "hello.js"}},
		{name: "es module javascript file wins", files: []string{"hello.mjs", "hello.ts"}, want: map[string]string{"hello": "hello.mjs"}},
		{name: "directory wins over file", files: []string{"hello.ts", "hello/index.js"}, want: map[string]string{"hello": "hello/index.js"}},
		{name: "typescript directory wins over typescript file", files: []string{"hello.ts", "hello/index.ts"}, want: map[string]string{"hello": "hello/index.ts"}},
		{name: "javascript file wins over typescript directory", files: []string{"hello.js", "hello/index.ts"}, want: map[string]string{"hello": "hello.js"}},
		{name: "python file wins over typescript directory", files: []string{"hello.py", "hello/index.mts"}, want: map[string]string{"hello": "hello.py"}, wantRuntime: "python3.12"},
		{name: "javascript file wins over directory javascript beside typescript", files: []string{"hello.js", "hello/index.mjs", "hello/index.ts"}, want: map[string]string{"hello": "hello.js"}},
		{name: "javascript file wins over typescript directory and file", files: []string{"hello.js", "hello.ts", "hello/index.ts"}, want: map[string]string{"hello": "hello.js"}},
		{name: "directory index", files: []string{"hello/index.ts", "hello/lib.ts", "hello/tsconfig.json"}, want: map[string]string{"hello": "hello/index.ts"}, wantRuntime: "nodejs24.x"},
		{name: "directory es module index", files: []string{"hello/index.mts"}, want: map[string]string{"hello": "hello/index.mts"}},
		{name: "directory index beside other javascript", files: []string{"hello/index.ts", "hello/handler.js"}, want: map[string]string{"hello": "hello/index.ts"}},
		{name: "directory python entrypoint wins", files: []string{"hello/index.ts", "hello/main.py"}, want: map[string]string{"hello": "hello/main.py"}, wantRuntime: "python3.12"},
		{name: "directory ruby entrypoint wins", files: []string{"hello/index.ts", "hello/main.rb"}, want: map[string]string{"hello": "hello/main.rb"}, wantRuntime: "ruby3.4"},
		{name: "directory javascript index wins", files: []string{"hello/index.js", "hello/index.ts"}, want: map[string]string{"hello": "hello/index.js"}},
		{name: "directory es module javascript index wins", files: []string{"hello/index.mjs", "hello/index.ts"}, want: map[string]string{"hello": "hello/index.mjs"}},
		{name: "directory commonjs javascript index wins", files: []string{"hello/index.cjs", "hello/index.ts"}, want: map[string]string{"hello": "hello/index.cjs"}},
		{name: "directory prefers indexes by name", files: []string{"hello/index.mts", "hello/index.ts"}, want: map[string]string{"hello": "hello/index.mts"}},
		{name: "directory prefers ts over tsx by name", files: []string{"hello/index.ts", "hello/index.tsx"}, want: map[string]string{"hello": "hello/index.ts"}},
		{name: "javascript index settles two typescript indexes", files: []string{"hello/index.mjs", "hello/index.ts", "hello/index.tsx"}, want: map[string]string{"hello": "hello/index.mjs"}},
		{name: "directory needs an index at its top", files: []string{"hello/src/index.ts", "hello/handler.ts"}, want: map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			functionsDir := filepath.Join(dir, "volcano", "functions")
			for _, file := range tc.files {
				path := filepath.Join(functionsDir, filepath.FromSlash(file))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
			}

			sources, err := ScanSources(dir, testRuntimeCatalog(t))
			require.NoError(t, err)

			got := map[string]string{}
			for _, source := range sources {
				rel, err := filepath.Rel(functionsDir, source.Path)
				require.NoError(t, err)
				got[source.Name] = filepath.ToSlash(rel)
				if tc.wantRuntime != "" {
					assert.Equal(t, tc.wantRuntime, source.Runtime.Name, source.Name)
				}
			}
			assert.Equal(t, tc.want, got)
			assert.Len(t, sources, len(tc.want), "one source per function")
		})
	}
}

func TestScanSourcesRejectsTwoTypeScriptFilesForOneFunction(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		// wantErr is the scan's error; with none, want is the one source's
		// path, relative to volcano/functions.
		wantErr string
		want    string
	}{
		{name: "ts and mts", files: []string{"hello.ts", "hello.mts"}, wantErr: `function "hello" has two TypeScript files, hello.mts and hello.ts, in volcano/functions; keep one`},
		{name: "cts and ts", files: []string{"hello.cts", "hello.ts"}, wantErr: `function "hello" has two TypeScript files, hello.cts and hello.ts, in volcano/functions; keep one`},
		{name: "ts and tsx", files: []string{"hello.ts", "hello.tsx"}, wantErr: `function "hello" has two TypeScript files, hello.ts and hello.tsx, in volcano/functions; keep one`},
		{name: "javascript settles it", files: []string{"hello.js", "hello.mts", "hello.ts", "hello.tsx"}, want: "hello.js"},
		{name: "a directory's two indexes are left to the build", files: []string{"hello/index.ts", "hello/index.tsx"}, want: "hello/index.ts"},
		{name: "a directory beats two typescript files", files: []string{"hello.ts", "hello.tsx", "hello/index.tsx"}, want: "hello/index.tsx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			functionsDir := filepath.Join(dir, "volcano", "functions")
			for _, file := range tc.files {
				path := filepath.Join(functionsDir, filepath.FromSlash(file))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
			}

			sources, err := ScanSources(dir, testRuntimeCatalog(t))
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, sources, 1)
			assert.Equal(t, filepath.Join(functionsDir, filepath.FromSlash(tc.want)), sources[0].Path)
		})
	}
}

func TestScanSourcesNeedsANodeRuntimeForTypeScript(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(filepath.Join(functionsDir, "hello-dir"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello.ts"), []byte(""), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello-dir", "index.ts"), []byte(""), 0o644))

	sources, err := ScanSources(dir, RuntimeCatalogFromOptions([]apiclient.FunctionRuntimeOption{
		testRuntimeOption("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
		testRuntimeOption("nodejs22.x", "nodejs", false, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json"}),
	}))
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestFindTargetNamesTypeScriptSource(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello.ts"), []byte(""), 0o644))

	sources, err := ScanSources(dir, testRuntimeCatalog(t))
	require.NoError(t, err)
	for _, target := range []string{"hello", filepath.Join("volcano", "functions", "hello.ts")} {
		source, found := FindTarget(sources, target, dir)
		require.True(t, found, target)
		assert.Equal(t, "hello", source.Name, target)
	}
}

// A path names the function whose file or directory it is in, even when its
// basename is another function's name: index.ts beside orders/ is function
// index, and orders/index.ts, shadowed by orders/index.js, is still orders. A
// path below widgets/, which is no function, never names widgets.js.
func TestFindTargetPrefersPathsOverNames(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	for _, file := range []string{"index.ts", "orders/index.js", "orders/index.ts", "widgets.js", "widgets/src/index.ts"} {
		path := filepath.Join(functionsDir, filepath.FromSlash(file))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(""), 0o644))
	}

	sources, err := ScanSources(dir, testRuntimeCatalog(t))
	require.NoError(t, err)
	require.Len(t, sources, 3)

	for target, want := range map[string]string{
		"index":  "index",
		"orders": "orders",
		filepath.Join("volcano", "functions", "index.ts"):           "index",
		filepath.Join("volcano", "functions", "orders"):             "orders",
		filepath.Join("volcano", "functions", "orders", "index.js"): "orders",
		filepath.Join("volcano", "functions", "orders", "index.ts"): "orders",
		filepath.Join(functionsDir, "orders", "index.ts"):           "orders",
	} {
		source, found := FindTarget(sources, target, dir)
		require.True(t, found, target)
		assert.Equal(t, want, source.Name, target)
	}
	for _, target := range []string{"missing", filepath.Join("volcano", "functions", "widgets", "src", "index.ts")} {
		_, found := FindTarget(sources, target, dir)
		assert.False(t, found, target)
	}
}
