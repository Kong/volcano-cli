package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type archivedEntry struct {
	name string
	dir  bool
	mode int64
	data string
}

// readArchive checks one archive as the API reads it and returns its entries.
func readArchive(t *testing.T, data []byte, limits UploadLimits) []archivedEntry {
	t.Helper()
	require.LessOrEqual(t, int64(len(data)), limits.Body, "compressed body")
	gz, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	gz.Multistream(false)
	expanded, err := io.ReadAll(gz)
	require.NoError(t, err, "gzip stream and its CRC must be valid")
	require.NoError(t, gz.Close())
	rest, err := io.ReadAll(gz)
	require.NoError(t, err)
	require.Empty(t, rest, "one gzip member")
	require.LessOrEqual(t, int64(len(expanded)), limits.Expanded, "uncompressed tar stream")

	reader := tar.NewReader(bytes.NewReader(expanded))
	paths := map[string]bool{}
	var entries []archivedEntry
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		name := strings.TrimSuffix(header.Name, "/")
		for p := name; p != "."; p = path.Dir(p) {
			paths[p] = true
		}
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		entries = append(entries, archivedEntry{name: name, dir: header.Typeflag == tar.TypeDir, mode: header.Mode, data: string(content)})
	}
	require.LessOrEqual(t, len(paths), limits.Entries, "files and directories, implied ones included")
	return entries
}

func collect(t *testing.T, directory string, limits UploadLimits) [][]byte {
	t.Helper()
	upload, err := OpenUpload(directory, limits)
	require.NoError(t, err)
	defer func() { require.NoError(t, upload.Close()) }()
	var bodies [][]byte
	require.NoError(t, upload.Each(func(archive []byte) error {
		bodies = append(bodies, bytes.Clone(archive))
		return nil
	}))
	return bodies
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return dir
}

func randomBytes(t *testing.T, n int) string {
	t.Helper()
	data := make([]byte, n)
	_, err := rand.Read(data)
	require.NoError(t, err)
	return string(data)
}

func TestUploadRoundTripsTreeInWalkOrder(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("long-name-", 15) + ".txt"
	dir := writeTree(t, map[string]string{
		"README.md":           "hello\n",
		"src/main.go":         "package main\n",
		"src/lib/util.go":     "package lib\n",
		"bin/run.sh":          "#!/bin/sh\necho run\n",
		"data/blob.bin":       "\x00\xff\x80\n",
		"data/" + long:        "pax header\n",
		"docs/über/notes.txt": "unicode\n",
	})
	require.NoError(t, os.Chmod(filepath.Join(dir, "bin", "run.sh"), 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "empty"), 0o750))

	bodies := collect(t, dir, ArchiveLimits)
	require.Len(t, bodies, 1)
	entries := readArchive(t, bodies[0], ArchiveLimits)

	var names []string
	files := map[string]archivedEntry{}
	for _, e := range entries {
		names = append(names, e.name)
		files[e.name] = e
	}
	assert.Equal(t, []string{
		"README.md", "bin", "bin/run.sh", "data", "data/blob.bin", "data/" + long,
		"docs", "docs/über", "docs/über/notes.txt", "empty", "src", "src/lib", "src/lib/util.go", "src/main.go",
	}, names)
	assert.Equal(t, "\x00\xff\x80\n", files["data/blob.bin"].data)
	assert.Equal(t, "pax header\n", files["data/"+long].data)
	assert.Equal(t, int64(0o755), files["bin/run.sh"].mode)
	assert.Equal(t, int64(0o644), files["README.md"].mode)
	assert.True(t, files["empty"].dir)
	assert.Equal(t, int64(0o750), files["empty"].mode)

	again := collect(t, dir, ArchiveLimits)
	assert.Equal(t, bodies, again, "the same tree packs the same bytes")
}

func TestUploadEmptyDirectoryMakesOneEmptyArchive(t *testing.T) {
	t.Parallel()
	bodies := collect(t, t.TempDir(), ArchiveLimits)
	require.Len(t, bodies, 1)
	assert.Empty(t, readArchive(t, bodies[0], ArchiveLimits))
}

// requireSplit checks that the archives carry every entry once, in walk order.
func requireSplit(t *testing.T, dir string, limits UploadLimits) [][]byte {
	t.Helper()
	whole := collect(t, dir, ArchiveLimits)
	require.Len(t, whole, 1)
	want := readArchive(t, whole[0], ArchiveLimits)

	bodies := collect(t, dir, limits)
	require.Greater(t, len(bodies), 1, "limits must split this tree")
	var got []archivedEntry
	for _, body := range bodies {
		entries := readArchive(t, body, limits)
		require.NotEmpty(t, entries)
		got = append(got, entries...)
	}
	assert.Equal(t, want, got)
	return bodies
}

func TestUploadSplitsAtTheEntryLimit(t *testing.T) {
	t.Parallel()
	tree := map[string]string{}
	for _, name := range []string{"a/1", "a/2", "a/3", "a/b/4", "a/b/5", "c/6", "c/7", "d/e/f/8"} {
		tree[name] = name
	}
	bodies := requireSplit(t, writeTree(t, tree), UploadLimits{Body: 1 << 20, Entries: 4, Expanded: 1 << 20})
	assert.GreaterOrEqual(t, len(bodies), 4)
}

func TestUploadSplitsAtTheExpandedLimit(t *testing.T) {
	t.Parallel()
	tree := map[string]string{}
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		tree[name] = strings.Repeat("x", 3000)
	}
	// Each file is 512 bytes of header and 3,072 of content and padding.
	limits := UploadLimits{Body: 1 << 20, Entries: 100, Expanded: 2*3584 + tarTrailer}
	bodies := requireSplit(t, writeTree(t, tree), limits)
	assert.Len(t, bodies, 3, "two files fill each archive")
}

func TestUploadSplitsAtTheCompressedLimit(t *testing.T) {
	t.Parallel()
	tree := map[string]string{}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		tree[name] = randomBytes(t, 6000)
	}
	limits := UploadLimits{Body: 20_000, Entries: 100, Expanded: 1 << 20}
	bodies := requireSplit(t, writeTree(t, tree), limits)
	assert.Len(t, bodies, 3, "three incompressible files fill each archive")
}

func TestUploadSendsACompressibleFileLargerThanTheBodyLimit(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"zeros": strings.Repeat("\x00", 200_000), "small": "small"})
	limits := UploadLimits{Body: 16 << 10, Entries: 100, Expanded: 1 << 20}
	bodies := collect(t, dir, limits)
	require.Len(t, bodies, 1)
	entries := readArchive(t, bodies[0], limits)
	require.Len(t, entries, 2)
	assert.Len(t, entries[1].data, 200_000)
}

func TestUploadRefusesBeforePacking(t *testing.T) {
	t.Parallel()
	small := UploadLimits{Body: 16 << 10, Entries: 3, Expanded: 64 << 10}
	tests := []struct {
		name   string
		setup  func(t *testing.T, dir string)
		limits UploadLimits
		want   string
	}{
		{
			name: "symbolic link to a file",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("x"), 0o600))
				require.NoError(t, os.Symlink("target", filepath.Join(dir, "link")))
			},
			limits: ArchiveLimits,
			want:   "link is a symbolic link; uploads copy only regular files and directories",
		},
		{
			name: "symbolic link to a directory outside",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o700))
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dir, "sub", "outside")))
			},
			limits: ArchiveLimits,
			want:   filepath.Join("sub", "outside") + " is a symbolic link",
		},
		{
			name: "file over the limit for parts",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				file, err := os.Create(filepath.Join(dir, "huge"))
				require.NoError(t, err)
				require.NoError(t, file.Truncate(MaxPartedBytes+1))
				require.NoError(t, file.Close())
			},
			limits: ArchiveLimits,
			want:   "huge is too large to upload: files are limited to 1 GiB",
		},
		{
			name: "path deeper than the entry limit",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "c", "d"), []byte("x"), 0o600))
			},
			limits: small,
			want:   filepath.Join("a", "b", "c", "d") + " is nested too deeply to upload",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tc.setup(t, dir)
			_, err := OpenUpload(dir, tc.limits)
			require.ErrorContains(t, err, tc.want)
			assert.Contains(t, err.Error(), dir, "names the local path")
		})
	}
}

func TestUploadStopsAtTheFirstSendError(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"a": "a", "b": "b", "c": "c"})
	upload, err := OpenUpload(dir, UploadLimits{Body: 1 << 20, Entries: 1, Expanded: 1 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, upload.Close()) }()
	sent := 0
	refused := errors.New("refused")
	err = upload.Each(func([]byte) error {
		sent++
		return refused
	})
	require.ErrorIs(t, err, refused)
	assert.Equal(t, 1, sent)
}

func TestUploadNoticesAFileThatChanged(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"grows": "short"})
	upload, err := OpenUpload(dir, ArchiveLimits)
	require.NoError(t, err)
	defer func() { require.NoError(t, upload.Close()) }()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "grows"), []byte("much longer now"), 0o600))
	err = upload.Each(func([]byte) error { return nil })
	require.ErrorContains(t, err, "grows changed while it was being uploaded")
}
