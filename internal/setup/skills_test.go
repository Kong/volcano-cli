package setup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMaterialize(t *testing.T) {
	srv := skillsServer(t)
	dir := t.TempDir()
	agents := filepath.Join(dir, "AGENTS.md")

	n, err := materialize(context.Background(), srv.Client(), srv.URL, filepath.Join(dir, "skills"), agents)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if n != 2 {
		t.Fatalf("wrote %d skills, want 2", n)
	}
	assertFile(t, filepath.Join(dir, "skills", "volcano-platform", "SKILL.md"), "Volcano skill content")
	assertFile(t, filepath.Join(dir, "skills", "install-volcano", "SKILL.md"), "Volcano skill content")
	assertFile(t, agents, "Volcano AGENTS.md")

	// SKILL.md is written owner read/write only (0600), matching repo convention.
	info, err := os.Stat(filepath.Join(dir, "skills", "volcano-platform", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("SKILL.md perm = %o, want 600", info.Mode().Perm())
	}
}

func TestFetchGET_Rejections(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"html": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "<!DOCTYPE html><html>oops</html>")
		},
		"empty":   func(http.ResponseWriter, *http.Request) {},
		"non-200": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if _, err := fetchGET(context.Background(), srv.Client(), srv.URL); err == nil {
				t.Fatalf("expected error for %s response", name)
			}
		})
	}
}

func TestMaterialize_RejectsBadSkillName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/skills/index.json", func(w http.ResponseWriter, _ *http.Request) {
		// A traversal name must not be turned into a filesystem path.
		_, _ = io.WriteString(w, `{"skills":[{"name":"../escape","path":"/skills/x/SKILL.md"}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if _, err := materialize(context.Background(), srv.Client(), srv.URL, t.TempDir(), ""); err == nil {
		t.Fatal("expected error for invalid skill name")
	}
}

func TestValidSkillName(t *testing.T) {
	ok := []string{"volcano-platform", "install-volcano", "a.b_c-1"}
	bad := []string{"", ".", "..", "../escape", "a/b", "a b", "a\\b"}
	for _, s := range ok {
		if !validSkillName(s) {
			t.Errorf("validSkillName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validSkillName(s) {
			t.Errorf("validSkillName(%q) = true, want false", s)
		}
	}
}
