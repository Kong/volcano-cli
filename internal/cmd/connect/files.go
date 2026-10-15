package connectcmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

type envVar struct {
	key, value string
}

// upsertEnv sets vars in the env file at path, keeping every other line and
// comment as it is. A key already in the file is updated in place, so a rerun
// never adds a second copy.
func upsertEnv(path string, vars []envVar) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	} else {
		lines = []string{"# Cloud project values, written by `volcano connect`. Deploy with:", "#   volcano cloud deploy --env-file volcano/cloud.env"}
	}
	for _, v := range vars {
		found := false
		for i, line := range lines {
			if envKey(line) == v.key {
				lines[i] = v.key + "=" + v.value
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, v.key+"="+v.value)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	//nolint:gosec // path is the env file under the directory the user asked connect to set up
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), envFileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// envKey returns the key a KEY=value line sets, or "" for anything else.
func envKey(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	line = strings.TrimPrefix(line, "export ")
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}

// gitignoreEntries keep the per-developer link and the env files, which hold
// keys, out of the repository.
var gitignoreEntries = []string{".volcano/project.json", "volcano/*.env"}

// ensureGitignored adds any missing gitignoreEntries to dir/.gitignore.
func ensureGitignored(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	present := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, entry := range gitignoreEntries {
		if !present[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
		buf.WriteString("\n")
	}
	if len(data) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("# Volcano: this checkout's project link, and env files that hold keys\n")
	buf.WriteString(strings.Join(missing, "\n") + "\n")
	if err := os.WriteFile(path, buf.Bytes(), fileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

const (
	agentsStartMarker = "<!-- volcano:connect:start -->"
	agentsEndMarker   = "<!-- volcano:connect:end -->"
)

func agentsSection(project *apiclient.Project, dashboardURL string) string {
	return fmt.Sprintf(`%s
## Volcano

This directory is linked to the Volcano cloud project %s (%s) in
.volcano/project.json. Volcano CLI commands run here use that project.

- Before writing code for this project, read the volcano-platform and
  volcano-sdk skills. If they are missing, run `+"`volcano setup`"+`.
- volcano/cloud.env holds the project's API URL and anon key. It is gitignored.
- Run locally: `+"`volcano start`"+`, then `+"`volcano deploy --with-migrations`"+`.
- Deploy to the cloud project: `+"`volcano cloud deploy --env-file volcano/cloud.env`"+`.
- Dashboard: %s
%s
`, agentsStartMarker, project.Name, project.Id, dashboardURL, agentsEndMarker)
}

// writeAgentsSection puts section in the AGENTS.md at path, replacing the one
// an earlier run wrote and keeping the rest of the file.
func writeAgentsSection(path, section string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	content := string(data)
	start := strings.Index(content, agentsStartMarker)
	end := strings.Index(content, agentsEndMarker)
	switch {
	case start >= 0 && end > start:
		end += len(agentsEndMarker)
		if end < len(content) && content[end] == '\n' {
			end++
		}
		content = content[:start] + section + content[end:]
	case content == "":
		content = section
	default:
		content = strings.TrimRight(content, "\n") + "\n\n" + section
	}
	//nolint:gosec // path is AGENTS.md in the directory the user asked connect to set up
	if err := os.WriteFile(path, []byte(content), fileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}
