package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func names(ds []Detected) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Name
	}
	return out
}

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
	if got, want := strings.Join(names(got), ","), "cursor,pi"; got != want {
		t.Fatalf("Detect names = %q, want %q", got, want)
	}
	for _, d := range got {
		if d.Installed {
			t.Fatalf("%s reported installed with no volcano skill present", d.Name)
		}
	}
}

func TestDetect_MarksInstalledWhenVolcanoSkillPresent(t *testing.T) {
	home := t.TempDir()
	// cursor detected AND a volcano skill already dropped -> installed.
	mustMkdir(t, filepath.Join(home, ".cursor", "skills", "volcano-platform"))
	// pi detected but only a non-volcano skill -> detected, not installed.
	mustMkdir(t, filepath.Join(home, ".pi", "agent", "skills", "some-other-skill"))

	got, err := Detect(Options{HomeDir: home, Getenv: emptyEnv, LookPath: noBins})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	installed := map[string]bool{}
	for _, d := range got {
		installed[d.Name] = d.Installed
	}
	if !installed["cursor"] {
		t.Fatalf("cursor should be installed (volcano-platform present); got %+v", got)
	}
	if installed["pi"] {
		t.Fatalf("pi should not be installed (only a non-volcano skill present); got %+v", got)
	}
}

func TestDetect_InstalledFollowsSymlinkedSkill(t *testing.T) {
	home := t.TempDir()
	// A skill dropped as a symlink to a real dir (some agents link a shared repo)
	// must still count as installed; DirEntry.IsDir() alone would miss it.
	target := filepath.Join(home, "shared", "volcano-platform")
	mustMkdir(t, target)
	skillsDir := filepath.Join(home, ".cursor", "skills")
	mustMkdir(t, skillsDir)
	if err := os.Symlink(target, filepath.Join(skillsDir, "volcano-platform")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := Detect(Options{HomeDir: home, Getenv: emptyEnv, LookPath: noBins})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, d := range got {
		if d.Name == "cursor" && !d.Installed {
			t.Fatalf("cursor with symlinked volcano skill should be installed; got %+v", got)
		}
	}
}

func TestDetectedLabel(t *testing.T) {
	if got := (Detected{Name: "cursor", Installed: true}).Label(); !strings.Contains(got, "[installed]") || !strings.Contains(got, "cursor") {
		t.Fatalf("installed label = %q, want [installed] + name", got)
	}
	if got := (Detected{Name: "cursor", Installed: false}).Label(); !strings.Contains(got, "[available]") {
		t.Fatalf("available label = %q, want [available]", got)
	}
}
