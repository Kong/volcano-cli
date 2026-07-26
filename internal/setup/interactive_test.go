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

func TestSelectHarnesses(t *testing.T) {
	detected := []string{"claude-code", "cursor", "pi"}
	tests := []struct {
		name  string
		input string
		want  []string // nil means "cancel"
	}{
		{"enter selects all", "\n", detected},
		{"eof selects all", "", detected},
		{"all keyword", "all\n", detected},
		{"yes selects all", "y\n", detected},
		{"no cancels", "n\n", nil},
		{"quit cancels", "quit\n", nil},
		{"single index", "2\n", []string{"cursor"}},
		{"comma subset", "1,3\n", []string{"claude-code", "pi"}},
		{"space subset out of order deduped", "3 1 1\n", []string{"pi", "claude-code"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SelectHarnesses(strings.NewReader(tt.input), &strings.Builder{}, detected)
			if err != nil {
				t.Fatalf("SelectHarnesses(%q): %v", tt.input, err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("SelectHarnesses(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSelectHarnesses_InvalidSelectionErrors(t *testing.T) {
	detected := []string{"claude-code", "cursor"}
	for _, input := range []string{"9\n", "0\n", "abc\n", "1,x\n"} {
		if _, err := SelectHarnesses(strings.NewReader(input), &strings.Builder{}, detected); err == nil {
			t.Fatalf("SelectHarnesses(%q): expected error, got nil", input)
		}
	}
}
