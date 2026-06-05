package api

import (
	"context"
	"debug/buildinfo"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildAPIE2EBinary(t *testing.T, apiURL string) string {
	t.Helper()
	if override := strings.TrimSpace(os.Getenv("VOLCANO_TEST_CLI_BINARY")); override != "" {
		binary := resolveAPIE2EPrebuiltBinary(t, override)
		requireAPIE2EPrebuiltBinary(t, binary, apiURL)
		return binary
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve current test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "volcano")
	ldflags := "-s -w -X github.com/Kong/volcano-cli/internal/config.compiledDefaultAPIURL=" + apiURL
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-ldflags", ldflags, "-o", binary, "./cmd/volcano")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build CLI binary: %v\n%s", err, strings.TrimSpace(string(output)))
	}
	return binary
}

func resolveAPIE2EPrebuiltBinary(t *testing.T, binary string) string {
	t.Helper()
	resolved, err := filepath.Abs(binary)
	if err != nil {
		t.Fatalf("failed to resolve VOLCANO_TEST_CLI_BINARY: %s (%v)", binary, err)
	}
	return resolved
}

func requireAPIE2EPrebuiltBinary(t *testing.T, binary, apiURL string) {
	t.Helper()
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("VOLCANO_TEST_CLI_BINARY does not exist: %s (%v)", binary, err)
	}
	if info.IsDir() {
		t.Fatalf("VOLCANO_TEST_CLI_BINARY is a directory: %s", binary)
	}

	compiledAPIURL, ok, err := apiE2ECompiledAPIURL(binary)
	if err != nil {
		t.Fatalf("failed to inspect VOLCANO_TEST_CLI_BINARY build info: %s (%v)", binary, err)
	}
	if !ok {
		t.Fatalf("VOLCANO_TEST_CLI_BINARY must be built with -ldflags \"-X %s=<url>\" matching VOLCANO_API_URL=%q", apiE2ECompiledAPIURLVar, apiURL)
	}
	if normalizeAPIE2EURL(compiledAPIURL) != normalizeAPIE2EURL(apiURL) {
		t.Fatalf("VOLCANO_TEST_CLI_BINARY targets API URL %q, but VOLCANO_API_URL is %q", compiledAPIURL, apiURL)
	}
}

func apiE2ECompiledAPIURL(binary string) (string, bool, error) {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return "", false, err
	}
	for _, setting := range info.Settings {
		if setting.Key == "-ldflags" {
			value, ok := apiE2ECompiledAPIURLFromLDFlags(setting.Value)
			return value, ok, nil
		}
	}
	return "", false, nil
}

func apiE2ECompiledAPIURLFromLDFlags(ldflags string) (string, bool) {
	fields := strings.Fields(ldflags)
	for i, field := range fields {
		if field == "-X" {
			if i+1 >= len(fields) {
				continue
			}
			if value, ok := strings.CutPrefix(fields[i+1], apiE2ECompiledAPIURLVar+"="); ok {
				return value, true
			}
			continue
		}
		if value, ok := strings.CutPrefix(field, "-X="+apiE2ECompiledAPIURLVar+"="); ok {
			return value, true
		}
	}
	return "", false
}

func TestResolveAPIE2EPrebuiltBinary(t *testing.T) {
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	got := resolveAPIE2EPrebuiltBinary(t, "."+string(filepath.Separator)+"volcano")
	want := filepath.Join(projectDir, "volcano")
	if got != want {
		t.Fatalf("binary = %q, want %q", got, want)
	}
}

func normalizeAPIE2EURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func TestAPIE2ECompiledAPIURLFromLDFlags(t *testing.T) {
	tests := []struct {
		name    string
		ldflags string
		want    string
		wantOK  bool
	}{
		{
			name:    "separate X flag",
			ldflags: "-s -w -X github.com/Kong/volcano-cli/internal/config.compiledDefaultAPIURL=https://api.example.test",
			want:    "https://api.example.test",
			wantOK:  true,
		},
		{
			name:    "equals X flag",
			ldflags: "-s -w -X=github.com/Kong/volcano-cli/internal/config.compiledDefaultAPIURL=https://api.example.test",
			want:    "https://api.example.test",
			wantOK:  true,
		},
		{
			name:    "missing API URL flag",
			ldflags: "-s -w -X github.com/Kong/volcano-cli/internal/version.Version=dev",
			wantOK:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := apiE2ECompiledAPIURLFromLDFlags(tt.ldflags)
			if gotOK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("url = %q, want %q", got, tt.want)
			}
		})
	}
}
