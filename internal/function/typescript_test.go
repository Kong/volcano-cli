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
		{name: "tsx is not compiled", files: []string{"widget.tsx", "page/index.tsx"}, want: map[string]string{}},
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
		name    string
		files   []string
		wantErr string
	}{
		{name: "ts and mts", files: []string{"hello.ts", "hello.mts"}, wantErr: `function "hello" has two TypeScript files, hello.mts and hello.ts, in volcano/functions; keep one`},
		{name: "cts and ts", files: []string{"hello.cts", "hello.ts"}, wantErr: `function "hello" has two TypeScript files, hello.cts and hello.ts, in volcano/functions; keep one`},
		{name: "javascript settles it", files: []string{"hello.js", "hello.mts", "hello.ts"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			functionsDir := filepath.Join(dir, "volcano", "functions")
			require.NoError(t, os.MkdirAll(functionsDir, 0o755))
			for _, file := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(functionsDir, file), []byte(""), 0o644))
			}

			sources, err := ScanSources(dir, testRuntimeCatalog(t))
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, sources, 1)
			assert.Equal(t, filepath.Join(functionsDir, "hello.js"), sources[0].Path)
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

func TestMatchesTargetNamesTypeScriptSource(t *testing.T) {
	dir := t.TempDir()
	functionsDir := filepath.Join(dir, "volcano", "functions")
	require.NoError(t, os.MkdirAll(functionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(functionsDir, "hello.ts"), []byte(""), 0o644))

	sources, err := ScanSources(dir, testRuntimeCatalog(t))
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.True(t, MatchesTarget(sources[0], "hello", dir))
	assert.True(t, MatchesTarget(sources[0], filepath.Join("volcano", "functions", "hello.ts"), dir))
}
