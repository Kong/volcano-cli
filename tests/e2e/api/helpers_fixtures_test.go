package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAPIE2EBaseProject(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "volcano.env"), "SMOKE_MESSAGE=hello-from-cli-e2e\n")
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "functions", "hello.js"), `
exports.handler = async () => {
  return { statusCode: 200, body: JSON.stringify({ message: process.env.SMOKE_MESSAGE || "hello" }) };
};
`)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "migrations", "001_cli_e2e.sql"), `
CREATE TABLE IF NOT EXISTS cli_e2e_smoke (
  id SERIAL PRIMARY KEY,
  message TEXT NOT NULL
);
`)
}

func writeAPIE2EFrontend(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EFile(t, filepath.Join(projectDir, "web", "package.json"), `{
  "scripts": {
    "build": "next build"
  },
  "dependencies": {
    "next": "15.5.9",
    "react": "18.3.1",
    "react-dom": "18.3.1"
  }
}`)
	writeAPIE2EFile(t, filepath.Join(projectDir, "web", "pages", "index.js"), `
export default function Home() {
  return <main>Volcano CLI E2E</main>;
}
`)
}

func writeAPIE2EFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
