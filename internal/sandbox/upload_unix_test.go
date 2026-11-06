//go:build unix

package sandbox

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadRefusesAFIFO(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600))
	_, err := OpenUpload(dir, ArchiveLimits)
	require.ErrorContains(t, err, filepath.Join(dir, "pipe")+" is not a regular file or directory")
}
