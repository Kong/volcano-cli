package localmode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalModeUsesSuppliedBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "volcano")
	if err := os.WriteFile(binary, []byte("supplied release artifact"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VOLCANO_TEST_CLI_BINARY", binary)
	if got := buildLocalModeE2EBinary(t); got != binary {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
}
