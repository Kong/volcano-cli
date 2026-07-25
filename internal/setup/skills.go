package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// HTTPDoer is the subset of *http.Client used to fetch skill content. It is
// satisfied by both http.DefaultClient and apiclient.HttpRequestDoer.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// maxDownload caps a single skill/AGENTS.md download to guard against a
// misconfigured origin streaming an unbounded body.
const maxDownload = 5 << 20 // 5 MiB

type skillIndex struct {
	Skills []struct {
		Name string `json:"name"`
	} `json:"skills"`
}

// materialize downloads the skills manifest from baseURL and writes each
// <name>/SKILL.md under skillsDir. When agentsPath is non-empty, AGENTS.md is
// written there too. Returns the number of skills written. baseURL points at the
// Kong/volcano-skills GitHub repo (raw), the single source of truth, so the CLI
// carries no copy of the skills to drift.
func materialize(ctx context.Context, doer HTTPDoer, baseURL, skillsDir, agentsPath string) (int, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	idxBody, err := fetchGET(ctx, doer, baseURL+"/index.json")
	if err != nil {
		return 0, fmt.Errorf("fetch skills index: %w", err)
	}
	var idx skillIndex
	if err := json.Unmarshal(idxBody, &idx); err != nil {
		return 0, fmt.Errorf("parse skills index: %w", err)
	}
	if len(idx.Skills) == 0 {
		return 0, fmt.Errorf("skills index %s/index.json listed no skills", baseURL)
	}

	if err := os.MkdirAll(skillsDir, 0o750); err != nil {
		return 0, err
	}

	count := 0
	for _, s := range idx.Skills {
		if !validSkillName(s.Name) {
			return count, fmt.Errorf("skills index contained an invalid skill name: %q", s.Name)
		}
		// Derive the URL from the validated name; the manifest's sibling `path`
		// field is deliberately ignored so a manifest cannot repoint the fetch at
		// another origin (e.g. a `path` of "@evil.example/x" turning the base into
		// userinfo).
		body, err := fetchGET(ctx, doer, baseURL+"/"+s.Name+"/SKILL.md")
		if err != nil {
			return count, fmt.Errorf("fetch skill %s: %w", s.Name, err)
		}
		dir := filepath.Join(skillsDir, s.Name)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return count, err
		}
		if err := writeFileAtomic(filepath.Join(dir, "SKILL.md"), body); err != nil {
			return count, err
		}
		count++
	}

	if agentsPath != "" {
		body, err := fetchGET(ctx, doer, baseURL+"/AGENTS.md")
		if err != nil {
			return count, fmt.Errorf("fetch AGENTS.md: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(agentsPath), 0o750); err != nil {
			return count, err
		}
		if err := writeFileAtomic(agentsPath, body); err != nil {
			return count, err
		}
	}

	return count, nil
}

// fetchGET performs a GET and returns the body, rejecting non-200, empty, and
// HTML responses (a misrouted request that lands on the web app's HTML shell).
func fetchGET(ctx context.Context, doer HTTPDoer, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := doer.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned status %d", url, resp.StatusCode)
	}
	// Read one byte past the cap so an oversized body is rejected rather than
	// silently truncated (io.LimitReader alone returns EOF at the limit).
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDownload {
		return nil, fmt.Errorf("GET %s exceeded the %d-byte limit", url, maxDownload)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("GET %s returned an empty body", url)
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("GET %s returned HTML, not skill content", url)
	}
	return body, nil
}

func looksLikeHTML(body []byte) bool {
	head := body
	if len(head) > 200 {
		head = head[:200]
	}
	s := strings.ToLower(string(head))
	return strings.Contains(s, "<!doctype html") || strings.Contains(s, "<html")
}

// validSkillName rejects names that would let a manifest escape skillsDir or
// carry path separators (trust boundary: the manifest drives filesystem writes).
func validSkillName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return name != "." && name != ".."
}

// writeFileAtomic writes data to path via a temp file + rename so a partial
// download never leaves a truncated SKILL.md behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".volcano-setup-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// os.CreateTemp already made the file 0600 (repo convention). os.Rename
	// replaces an existing destination on every platform Go targets — Windows
	// included, via MoveFileEx — so a rerun overwrites cleanly; temp+rename keeps
	// a partial download from surfacing as a truncated SKILL.md. (Rename is not
	// atomic on Windows, but the replace is still safe for a rerun.)
	return os.Rename(tmpName, path)
}
