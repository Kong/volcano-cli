//go:build unix

package function

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageSourceSkipsSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	functionDir := filepath.Join(dir, "volcano", "functions", "hello")
	require.NoError(t, os.MkdirAll(functionDir, 0o755))
	entryPath := filepath.Join(functionDir, "index.js")
	require.NoError(t, os.WriteFile(entryPath, []byte("exports.handler = async () => ({ statusCode: 200 });"), 0o644))

	socketPath := filepath.Join(functionDir, "events.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Skipf("unix socket unavailable: %v", err)
	}
	defer listener.Close()

	pkg, err := PackageSource(SourceInfo{
		Path:    entryPath,
		Name:    "hello",
		Runtime: testRuntimeOption("nodejs24.x", "nodejs", true, nil, "index.js", "handler", nil),
		IsDir:   true,
	}, dir)
	require.NoError(t, err)

	assert.NotContains(t, packageArchiveNames(t, pkg.ArchiveData), "events.sock")
}
