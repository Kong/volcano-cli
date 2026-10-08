package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageDirectoryPreservesBuildContextAndExcludesSecrets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, content := range map[string]string{"Dockerfile": "FROM python:3.12-slim\nCOPY app.py /app.py\n", "app.py": "print(42)", ".env": "secret", ".gitignore": "ignored.txt\n", "ignored.txt": "ignored", ".dockerignore": "ignored.txt\n"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	archive, err := PackageDirectory(dir)
	require.NoError(t, err)
	second, err := PackageDirectory(dir)
	require.NoError(t, err)
	require.Equal(t, archive, second, "same build context must keep the same idempotency body")
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]string{}
	for {
		header, readErr := tr.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		require.NoError(t, readErr)
		data, readErr := io.ReadAll(tr)
		require.NoError(t, readErr)
		files[header.Name] = string(data)
	}
	require.Equal(t, map[string]string{"Dockerfile": "FROM python:3.12-slim\nCOPY app.py /app.py\n", "app.py": "print(42)", ".dockerignore": "ignored.txt\n"}, files)
}

func TestPackageDirectoryRefusesMissingDockerfileSymlinkAndOversize(t *testing.T) {
	t.Parallel()
	t.Run("missing Dockerfile", func(t *testing.T) {
		t.Parallel()
		_, err := PackageDirectory(t.TempDir())
		require.ErrorContains(t, err, "root Dockerfile")
	})
	t.Run("symlink", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "Dockerfile")))
		_, err := PackageDirectory(dir)
		require.ErrorContains(t, err, "unsupported file")
	})
	t.Run("size", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		file, err := os.Create(filepath.Join(dir, "Dockerfile"))
		require.NoError(t, err)
		require.NoError(t, file.Truncate(MaxSourceBytes+1))
		require.NoError(t, file.Close())
		_, err = PackageDirectory(dir)
		require.ErrorContains(t, err, "32 MiB")
	})
}
