package testbinary

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolve(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("volcano", []byte("release"), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve("volcano")
	if err != nil || got != filepath.Join(directory, "volcano") {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	for _, path := range []string{"missing", "."} {
		if _, err := Resolve(path); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod("volcano", 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Resolve("volcano"); err == nil {
			t.Error("accepted non-executable file")
		}
	}
}
