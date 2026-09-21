// Package testbinary validates external CLI artifacts for acceptance tests.
package testbinary

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Resolve accepts release binaries without imposing build-time test settings.
func Resolve(binary string) (string, error) {
	resolved, err := filepath.Abs(binary)
	if err != nil {
		return "", fmt.Errorf("resolve VOLCANO_TEST_CLI_BINARY: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat VOLCANO_TEST_CLI_BINARY: %w", err)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		return "", fmt.Errorf("VOLCANO_TEST_CLI_BINARY must be an executable regular file: %s", resolved)
	}
	return resolved, nil
}
