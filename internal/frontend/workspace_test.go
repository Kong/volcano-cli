package frontend

import "testing"

func TestWorkspaceContains(t *testing.T) {
	tests := []struct {
		name    string
		globs   []string
		relPath string
		want    bool
	}{
		{
			name:    "single star matches direct child",
			globs:   []string{"apps/*"},
			relPath: "apps/web",
			want:    true,
		},
		{
			name:    "single star does not cross separators",
			globs:   []string{"apps/*"},
			relPath: "apps/web/inner",
			want:    false,
		},
		{
			name:    "doublestar matches recursive child",
			globs:   []string{"packages/**"},
			relPath: "packages/foo/bar",
			want:    true,
		},
		{
			name:    "doublestar matches direct child",
			globs:   []string{"packages/**"},
			relPath: "packages/foo",
			want:    true,
		},
		{
			name:    "middle doublestar matches any depth",
			globs:   []string{"apps/**/web"},
			relPath: "apps/team1/region/web",
			want:    true,
		},
		{
			name:    "middle doublestar matches zero segments",
			globs:   []string{"apps/**/web"},
			relPath: "apps/web",
			want:    true,
		},
		{
			name:    "middle doublestar requires trailing segment",
			globs:   []string{"apps/**/web"},
			relPath: "apps/team1/api",
			want:    false,
		},
		{
			name:    "doublestar does not match outside the prefix",
			globs:   []string{"packages/**"},
			relPath: "apps/web",
			want:    false,
		},
		{
			name:    "negation removes earlier match",
			globs:   []string{"packages/**", "!packages/example"},
			relPath: "packages/example",
			want:    false,
		},
		{
			name:    "negation does not affect unrelated paths",
			globs:   []string{"packages/**", "!packages/example"},
			relPath: "packages/foo",
			want:    true,
		},
		{
			name:    "literal path matches exactly",
			globs:   []string{"apps/web"},
			relPath: "apps/web",
			want:    true,
		},
		{
			name:    "literal path does not match descendants",
			globs:   []string{"apps/web"},
			relPath: "apps/web/inner",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceContains(tc.globs, tc.relPath); got != tc.want {
				t.Fatalf("workspaceContains(%v, %q) = %v, want %v", tc.globs, tc.relPath, got, tc.want)
			}
		})
	}
}
