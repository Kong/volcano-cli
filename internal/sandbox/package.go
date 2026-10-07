// Package sandbox packages custom Sandbox build contexts.
package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Kong/volcano-cli/internal/ignore"
)

// MaxSourceBytes is the public compressed and expanded source limit.
const MaxSourceBytes = 32 << 20

// PackageDirectory creates a bounded, deterministic archive rooted at directory.
// Symlinks are rejected so an uploaded build never silently omits dependencies.
func PackageDirectory(directory string) ([]byte, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	matcher, err := ignore.NewProjectMatcher(directory, ".env", ".env.*", "node_modules")
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	limited := &boundedWriter{writer: gz, remaining: MaxSourceBytes}
	tw := tar.NewWriter(limited)
	foundDockerfile := false
	files := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		if matcher.ShouldIgnore(name, entry.IsDir()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("build context contains unsupported file %q; use regular files", name)
		}
		files++
		if files > 10000 {
			return errors.New("build context exceeds 10000 files")
		}
		if name == "Dockerfile" {
			foundDockerfile = true
		}
		if info.Size() > limited.remaining {
			return errors.New("sandbox source exceeds 32 MiB")
		}
		return addFile(root, tw, name, info)
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, err
	}
	if !foundDockerfile {
		_ = tw.Close()
		_ = gz.Close()
		return nil, errors.New("build context must contain a root Dockerfile that is not ignored")
	}
	if err = tw.Close(); err != nil {
		_ = gz.Close()
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if compressed.Len() > MaxSourceBytes {
		return nil, errors.New("sandbox source exceeds 32 MiB")
	}
	return compressed.Bytes(), nil
}

func addFile(root *os.Root, tw *tar.Writer, name string, info fs.FileInfo) error {
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	header := &tar.Header{Name: filepath.ToSlash(name), Mode: int64(info.Mode().Perm()), Size: info.Size(), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.CopyN(tw, file, info.Size())
	return err
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, errors.New("sandbox source exceeds 32 MiB")
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	return n, err
}
