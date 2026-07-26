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

func TestDetectedStatusMark(t *testing.T) {
	cases := []struct {
		name string
		d    Detected
		want string
	}{
		{"installed current", Detected{Installed: true}, "[installed]"},
		{"available", Detected{Installed: false}, "[available]"},
		{"installed at latest", Detected{Installed: true, InstalledVersion: "0.2.16", LatestVersion: "0.2.16"}, "[installed]"},
		{"installed but behind", Detected{Installed: true, InstalledVersion: "0.2.14", LatestVersion: "0.2.16"}, "[outdated]"},
		// Installed ahead of a stale cache must not read as outdated.
		{"installed ahead of cache", Detected{Installed: true, InstalledVersion: "0.3.0", LatestVersion: "0.2.16"}, "[installed]"},
		// Not installed can never be outdated, even with version noise.
		{"available with versions", Detected{Installed: false, InstalledVersion: "0.2.14", LatestVersion: "0.2.16"}, "[available]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.TrimRight(tc.d.StatusMark(), " "); got != tc.want {
				t.Errorf("StatusMark = %q, want %q", got, tc.want)
			}
			// Every mark pads to the same width so picker rows align.
			if got := len(tc.d.StatusMark()); got != 11 {
				t.Errorf("mark width = %d, want 11 (%q)", got, tc.d.StatusMark())
			}
		})
	}
}

func TestDetectedVersionNote(t *testing.T) {
	if got := (Detected{Installed: true, InstalledVersion: "0.2.14", LatestVersion: "0.2.16"}).VersionNote(); got != " (0.2.14 \u2192 0.2.16 available)" {
		t.Errorf("outdated note = %q", got)
	}
	if got := (Detected{Installed: true, InstalledVersion: "0.2.16", LatestVersion: "0.2.16"}).VersionNote(); got != "" {
		t.Errorf("up-to-date note = %q, want empty", got)
	}
	if got := (Detected{Installed: false}).VersionNote(); got != "" {
		t.Errorf("available note = %q, want empty", got)
	}
}

// Detect must surface the installed/cached versions for a marketplace harness so
// the picker can flag it outdated. Seeds claude's registry + marketplace cache
// with a behind-latest version and puts claude on PATH.
func TestDetect_MarketplaceVersionsFlagOutdated(t *testing.T) {
	home := t.TempDir()
	seedClaudeVersions(t, home, "0.2.14", "0.2.16")
	lookClaude := func(bin string) (string, error) {
		if bin == "claude" {
			return "/usr/bin/claude", nil
		}
		return "", os.ErrNotExist
	}

	got, err := Detect(Options{HomeDir: home, Getenv: emptyEnv, LookPath: lookClaude})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var claude *Detected
	for i := range got {
		if got[i].Name == "claude-code" {
			claude = &got[i]
		}
	}
	if claude == nil {
		t.Fatalf("claude-code not detected: %+v", got)
	}
	if claude.InstalledVersion != "0.2.14" || claude.LatestVersion != "0.2.16" {
		t.Fatalf("versions = %q/%q, want 0.2.14/0.2.16", claude.InstalledVersion, claude.LatestVersion)
	}
	if !claude.Updatable() {
		t.Fatalf("claude with 0.2.14 installed and 0.2.16 available should be updatable")
	}
}
