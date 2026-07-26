package setup

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect_ReturnsPresentHarnessesInOrder(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".cursor"))
	mustMkdir(t, filepath.Join(home, ".pi", "agent"))

	got, err := Detect(Options{HomeDir: home, Getenv: emptyEnv, LookPath: noBins})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// harnesses() order is claude-code, codex, cursor, opencode, pi; only the
	// two dir-based ones exist here and no binaries are on PATH.
	want := []string{"cursor", "pi"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Detect = %v, want %v", got, want)
	}
}
