package frontend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageDirectoryStandaloneApp(t *testing.T) {
	root := t.TempDir()
	writePackageJSON(t, root, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"next": "15.5.9",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, "page.tsx"), []byte("export default null"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "node_modules", "foo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "node_modules", "foo", "index.js"), []byte("ignored"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte("PUBLIC=ok"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env.local"), []byte("SECRET=oops"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".env.production.local"), []byte("SECRET=oops"), 0o644))

	pkg, err := PackageDirectory(root, PackageOptions{})
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Empty(t, pkg.AppPath, "no workspace -> no app path marker")

	entries := readTarEntries(t, pkg.Archive)
	assert.Contains(t, entries, "package.json")
	assert.Contains(t, entries, "page.tsx")
	assert.Contains(t, entries, ".env", ".env should still be packaged")
	for _, name := range entries {
		assert.NotContains(t, name, "node_modules", "node_modules should be ignored")
		assert.NotEqual(t, ".env.local", name, ".env.local must not be uploaded")
		assert.NotEqual(t, ".env.production.local", name, ".env.production.local must not be uploaded")
	}
}

func TestPackageDirectoryPromotesToWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	writePackageJSON(t, root, map[string]any{
		"name":       "monorepo",
		"workspaces": []string{"apps/*"},
	})
	require.NoError(t, os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages: [\"apps/*\"]\n"), 0o644))

	appDir := filepath.Join(root, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"@org/shared": "workspace:*",
			"next":        "15.5.9",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	sharedDir := filepath.Join(root, "packages", "shared")
	require.NoError(t, os.MkdirAll(sharedDir, 0o755))
	writePackageJSON(t, sharedDir, map[string]any{"name": "@org/shared"})
	require.NoError(t, os.WriteFile(filepath.Join(sharedDir, "index.ts"), []byte("export const a = 1"), 0o644))

	pkg, err := PackageDirectory(appDir, PackageOptions{})
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Equal(t, "apps/web", pkg.AppPath)
	assert.Equal(t, root, pkg.PackagingRoot)

	entries := readTarEntries(t, pkg.Archive)
	assert.Contains(t, entries, "apps/web/page.tsx")
	assert.Contains(t, entries, "packages/shared/index.ts")
	assert.Contains(t, entries, frontendAppPathMarker)
}

func TestPackageDirectoryStandaloneAppWithoutWorkspaceProtocol(t *testing.T) {
	// If the app does not declare workspace: deps, packaging stays at the
	// selected root even when there is a workspace marker above.
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	writePackageJSON(t, root, map[string]any{
		"name":       "monorepo",
		"workspaces": []string{"apps/*"},
	})

	appDir := filepath.Join(root, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"next": "15.5.9",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	pkg, err := PackageDirectory(appDir, PackageOptions{})
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Empty(t, pkg.AppPath)
	assert.Equal(t, appDir, pkg.PackagingRoot)
	entries := readTarEntries(t, pkg.Archive)
	assert.Contains(t, entries, "page.tsx")
	for _, name := range entries {
		assert.NotEqual(t, frontendAppPathMarker, name)
	}
}

func TestPackageDirectoryDisableWorkspacePromotion(t *testing.T) {
	// When the caller is being explicit about the app layout (via --app-root),
	// auto-promotion is suppressed so the archive root matches the selected
	// directory.
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	writePackageJSON(t, root, map[string]any{
		"name":       "monorepo",
		"workspaces": []string{"apps/*"},
	})
	appDir := filepath.Join(root, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"@org/shared": "workspace:*",
			"next":        "15.5.9",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	pkg, err := PackageDirectory(appDir, PackageOptions{DisableWorkspacePromotion: true})
	require.NoError(t, err)
	require.NotNil(t, pkg)
	assert.Empty(t, pkg.AppPath, "no marker when promotion is disabled")
	assert.Equal(t, appDir, pkg.PackagingRoot)
	entries := readTarEntries(t, pkg.Archive)
	assert.Contains(t, entries, "page.tsx")
	for _, name := range entries {
		assert.NotEqual(t, frontendAppPathMarker, name)
	}
}

func TestPackageDirectoryDoesNotPromoteWithoutGitRoot(t *testing.T) {
	// Without a .git ancestor the workspace walk gives up before reaching a
	// stray workspace declaration, so packaging stays at the selected root.
	// This protects users with leftover workspaces fields in $HOME or other
	// non-repo ancestors from accidentally archiving the whole tree.
	root := t.TempDir()
	writePackageJSON(t, root, map[string]any{
		"name":       "stray",
		"workspaces": []string{"apps/*"},
	})
	appDir := filepath.Join(root, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"@org/shared": "workspace:*",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	pkg, err := PackageDirectory(appDir, PackageOptions{})
	require.NoError(t, err)
	assert.Empty(t, pkg.AppPath, "no .git -> no promotion")
	assert.Equal(t, appDir, pkg.PackagingRoot)
}

func TestPackageDirectoryDoesNotPromoteWhenAppOutsideWorkspaceGlobs(t *testing.T) {
	// The workspace declares apps/*, but the selected app lives under
	// packages/. Membership check must reject the promotion so the archive
	// does not balloon to include unrelated workspaces.
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	writePackageJSON(t, root, map[string]any{
		"name":       "monorepo",
		"workspaces": []string{"apps/*"},
	})
	appDir := filepath.Join(root, "packages", "shared")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "shared",
		"dependencies": map[string]any{
			"@org/util": "workspace:*",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	pkg, err := PackageDirectory(appDir, PackageOptions{})
	require.NoError(t, err)
	assert.Empty(t, pkg.AppPath, "non-member -> no promotion")
	assert.Equal(t, appDir, pkg.PackagingRoot)
}

func TestPackageDirectoryRejectsAppRootExcludedByMatcher(t *testing.T) {
	// User passes --app-root build but our cloudIgnorePatterns ignores
	// build/, so the upload would not contain the directory the server is
	// being told to compile from.
	root := t.TempDir()
	writePackageJSON(t, root, map[string]any{"name": "monorepo"})
	require.NoError(t, os.MkdirAll(filepath.Join(root, "build"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "build", "package.json"), []byte(`{"name":"app"}`), 0o644))

	_, err := PackageDirectory(root, PackageOptions{
		DisableWorkspacePromotion: true,
		AppRoot:                   "build",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "excluded by the archive")
}

func TestPackageDirectoryRejectsAppPathMarkerCollision(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o755))
	writePackageJSON(t, root, map[string]any{
		"name":       "monorepo",
		"workspaces": []string{"apps/*"},
	})
	appDir := filepath.Join(root, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	writePackageJSON(t, appDir, map[string]any{
		"name": "web",
		"dependencies": map[string]any{
			"@org/shared": "workspace:*",
		},
	})
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".volcano", "frontend-app-path"), []byte("apps/other\n"), 0o644))

	_, err := PackageDirectory(appDir, PackageOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frontend-app-path")
}

func TestPackageDirectoryReportsSkippedSymlinks(t *testing.T) {
	if _, err := os.Lstat("/dev/null"); err != nil {
		t.Skip("symlink test requires a working /dev/null")
	}
	root := t.TempDir()
	writePackageJSON(t, root, map[string]any{"name": "web"})
	require.NoError(t, os.WriteFile(filepath.Join(root, "page.tsx"), []byte("export default null"), 0o644))
	if err := os.Symlink("/dev/null", filepath.Join(root, "data-link")); err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	pkg, err := PackageDirectory(root, PackageOptions{})
	require.NoError(t, err)
	assert.Contains(t, pkg.SkippedSymlinks, "data-link")
	entries := readTarEntries(t, pkg.Archive)
	for _, name := range entries {
		assert.NotEqual(t, "data-link", name, "symlink target should not appear in archive")
	}
}

func TestPackageDirectoryEnforcesFileCountCap(t *testing.T) {
	// A runaway tree must error out rather than balloon the in-memory archive
	// past the configured limit.
	root := t.TempDir()
	writePackageJSON(t, root, map[string]any{"name": "web"})
	require.NoError(t, os.WriteFile(filepath.Join(root, "page.tsx"), []byte("export default null"), 0o644))

	bulk := filepath.Join(root, "bulk")
	require.NoError(t, os.Mkdir(bulk, 0o755))
	for i := range maxFrontendPackageFiles + 1 {
		require.NoError(t, os.WriteFile(filepath.Join(bulk, fmt.Sprintf("f%05d", i)), []byte("x"), 0o644))
	}

	_, err := PackageDirectory(root, PackageOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many files")
}

func TestPackageDirectoryRejectsNonDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "page.tsx")
	require.NoError(t, os.WriteFile(file, []byte("hi"), 0o644))

	_, err := PackageDirectory(file, PackageOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a directory")
}

func writePackageJSON(t *testing.T, dir string, payload map[string]any) {
	t.Helper()
	raw := mustMarshal(t, payload)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), raw, 0o644))
}

func mustMarshal(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func readTarEntries(t *testing.T, archive []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	return names
}
