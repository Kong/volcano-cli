package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Kong/volcano-cli/tests/e2e/testbinary"
)

func buildAPIE2EBinary(t *testing.T, apiURL string) string {
	t.Helper()
	if override := strings.TrimSpace(os.Getenv("VOLCANO_TEST_CLI_BINARY")); override != "" {
		binary, err := testbinary.Resolve(override)
		if err != nil {
			t.Fatal(err)
		}
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

func TestAPIE2EUsesSuppliedBinary(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "volcano")
	if err := os.WriteFile(binary, []byte("supplied release artifact"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VOLCANO_TEST_CLI_BINARY", binary)
	if got := buildAPIE2EBinary(t, "http://127.0.0.1:8000"); got != binary {
		t.Fatalf("binary = %q, want %q", got, binary)
	}
}

func TestAPIE2ECommandUsesExplicitAPI(t *testing.T) {
	t.Setenv("VOLCANO_API_URL", "https://api.volcano.dev")
	test := &apiE2E{apiURL: "http://127.0.0.1:8000", homeDir: t.TempDir()}
	var urls []string
	for _, entry := range test.commandEnv() {
		if strings.HasPrefix(entry, "VOLCANO_API_URL=") {
			urls = append(urls, entry)
		}
	}
	if len(urls) != 1 || urls[0] != "VOLCANO_API_URL="+test.apiURL {
		t.Fatalf("API environment = %v", urls)
	}
}

func TestAPIE2EInstalledBinaryUsesTestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/functions/runtimes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"runtimes":[{"name":"acceptance-test-runtime","language":"nodejs","default":true,"deployment":{"file_extensions":[".js"],"entrypoint":"index.js","handler":"handler","dependency_manifests":["package.json"]}}]}`))
	}))
	defer server.Close()
	env := &apiE2E{binary: buildAPIE2EBinary(t, server.URL), apiURL: server.URL, homeDir: t.TempDir(), projectDir: t.TempDir()}
	env.runCloudCLI(t, "functions", "runtimes").requireSuccess(t, "acceptance-test-runtime")
}
