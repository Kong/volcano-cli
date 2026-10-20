package connectcmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kong/volcano-cli/internal/symlink"
)

// gitignoreEntries keep the per-developer link and the env files, which hold
// keys, out of the repository.
var gitignoreEntries = []string{".volcano/project.json", "volcano/*.env"}

// ensureGitignored adds any missing gitignoreEntries to dir/.gitignore.
func ensureGitignored(dir string) error {
	if err := symlink.Refuse(dir, ".gitignore"); err != nil {
		return err
	}
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

// agentsSection is the same for every developer: AGENTS.md is usually
// committed, while the project it would name is each developer's own, in the
// gitignored link.
func agentsSection(dashboardURL string) string {
	return fmt.Sprintf(`%s
## Volcano

This directory is linked to a Volcano cloud project in .volcano/project.json
(gitignored). Volcano CLI commands run here use that project. If the file is
missing, run `+"`volcano connect .`"+` here.

- Before writing code for this project, read the volcano-platform and
  volcano-sdk skills. If they are missing, run `+"`volcano setup`"+`.
- volcano/cloud.env holds the project's API URL and anon key. It is gitignored.
- Run locally: `+"`volcano start`"+`, then `+"`volcano deploy --with-migrations`"+`.
- Deploy to the cloud project: `+"`volcano cloud deploy --env-file volcano/cloud.env`"+`.
- Dashboard: %s
%s
`, agentsStartMarker, dashboardURL, agentsEndMarker)
}

// writeAgentsSection puts section in dir's AGENTS.md, replacing the one an
// earlier run wrote and keeping the rest of the file.
func writeAgentsSection(dir, section string) error {
	if err := symlink.Refuse(dir, agentsFileName); err != nil {
		return err
	}
	path := filepath.Join(dir, agentsFileName)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	content := string(data)
	start := strings.Index(content, agentsStartMarker)
	end := -1
	if start >= 0 {
		if i := strings.Index(content[start:], agentsEndMarker); i >= 0 {
			end = start + i
		}
	}
	switch {
	case start >= 0 && end < 0:
		// Replacing up to a later end marker, or appending another section
		// that a later run would pair with this start, would delete what the
		// user wrote after it.
		return fmt.Errorf("%s has the Volcano section's start marker %s but no end marker %s after it. "+
			"Add the end marker where the section ends, or delete the section, and try again", path, agentsStartMarker, agentsEndMarker)
	case start >= 0:
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
