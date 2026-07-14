package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// HTTPDoer is the minimal HTTP contract used by sync, satisfied by
// *http.Client and the runtime's injected client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Conservative corpus limits. The Volcano docs corpus is ~96 files / ~800KB;
// these caps leave generous headroom while bounding a hostile or misconfigured
// source.
const (
	maxFiles     = 2000
	maxFileSize  = 4 << 20  // 4 MiB per file
	maxTotalSize = 64 << 20 // 64 MiB total
	syncWorkers  = 8        // bounded download concurrency
	realAPIHost  = "api.github.com"
)

// Syncer fetches a documentation corpus from GitHub into a Cache.
type Syncer struct {
	doer   HTTPDoer
	apiURL string
	rawURL string
	token  string
	now    func() time.Time
	cache  *Cache
	src    SourceRef
}

// SyncResult summarizes a completed synchronization.
type SyncResult struct {
	ResolvedCommit string `json:"resolved_commit"`
	Added          int    `json:"added"`
	Updated        int    `json:"updated"`
	Removed        int    `json:"removed"`
	Unchanged      int    `json:"unchanged"`
	Changed        bool   `json:"changed"`
}

// blob is one enumerated markdown document, path relative to the docs root.
type blob struct {
	Rel  string
	SHA  string
	Size int64
}

// Sync resolves the source ref to a commit, enumerates markdown docs, reuses
// unchanged blobs from the prior snapshot, downloads changed ones, and
// atomically publishes a new snapshot. A no-change sync only refreshes
// checked_at. Any failure leaves the previous cache untouched.
func (s *Syncer) Sync(ctx context.Context, force bool) (*SyncResult, error) {
	if err := s.src.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}

	commit, err := s.resolveCommit(ctx)
	if err != nil {
		return nil, err
	}

	prev, _ := s.cache.Load() // nil when no cache yet
	if !force && prev != nil && prev.ResolvedCommit == commit {
		s.cache.touch(s.now())
		return &SyncResult{ResolvedCommit: commit, Unchanged: len(prev.Files), Changed: false}, nil
	}

	blobs, err := s.enumerate(ctx, commit)
	if err != nil {
		return nil, err
	}
	if len(blobs) == 0 {
		return nil, fmt.Errorf("%w: no markdown docs found at %s/%s@%s", ErrSourceUnavailable, s.src.Repo, s.src.Path, commit)
	}

	prevSHA := map[string]string{}
	var prevSnap string
	if prev != nil {
		for _, f := range prev.Files {
			prevSHA[f.Path] = f.SHA
		}
		if snap, serr := s.cache.currentSnapshot(); serr == nil {
			prevSnap = snap
		}
	}

	stage, err := s.cache.newStaging(s.now())
	if err != nil {
		return nil, err
	}
	// discard staging unless we explicitly publish.
	published := false
	defer func() {
		if !published {
			stage.discard()
		}
	}()

	result := &SyncResult{ResolvedCommit: commit, Changed: true}

	// Partition into reuse (unchanged SHA) and download sets.
	var toDownload []blob
	for _, b := range blobs {
		if oldSHA, ok := prevSHA[b.Rel]; ok && oldSHA == b.SHA && prevSnap != "" {
			if err := stage.copyFrom(prevSnap, b.Rel); err == nil {
				result.Unchanged++
				continue
			}
			// fall through to re-download if reuse failed
		}
		toDownload = append(toDownload, b)
	}

	if err := s.download(ctx, commit, toDownload, stage); err != nil {
		return nil, err
	}
	for _, b := range toDownload {
		if _, existed := prevSHA[b.Rel]; existed {
			result.Updated++
		} else {
			result.Added++
		}
	}
	for oldPath := range prevSHA {
		found := false
		for _, b := range blobs {
			if b.Rel == oldPath {
				found = true
				break
			}
		}
		if !found {
			result.Removed++
		}
	}

	files := make([]FileEntry, 0, len(blobs))
	for _, b := range blobs {
		files = append(files, FileEntry{Path: b.Rel, SHA: b.SHA, Size: b.Size})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	now := s.now()
	manifest := &Manifest{
		SchemaVersion:  ManifestSchemaVersion,
		Provider:       Provider,
		Source:         s.src,
		ResolvedCommit: commit,
		Files:          files,
		SyncedAt:       now,
		CheckedAt:      now,
	}
	if err := stage.publish(manifest); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSyncIncomplete, err)
	}
	published = true
	return result, nil
}

// resolveCommit turns a ref (branch/tag/sha) into an immutable commit sha.
func (s *Syncer) resolveCommit(ctx context.Context) (string, error) {
	u := fmt.Sprintf("%s/repos/%s/commits/%s", s.apiURL, s.src.Repo, url.PathEscape(s.src.Ref))
	var out struct {
		SHA string `json:"sha"`
	}
	if err := s.getJSON(ctx, u, &out); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.SHA) == "" {
		return "", fmt.Errorf("%w: could not resolve ref %q", ErrSourceUnavailable, s.src.Ref)
	}
	return out.SHA, nil
}

// enumerate lists markdown blobs under the docs path at the given commit,
// preferring the recursive git-tree API and falling back to directory walking
// when GitHub truncates the response.
func (s *Syncer) enumerate(ctx context.Context, commit string) ([]blob, error) {
	u := fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", s.apiURL, s.src.Repo, commit)
	var tree struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := s.getJSON(ctx, u, &tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		return s.walkContents(ctx, commit, s.src.Path)
	}

	prefix := s.src.Path
	var blobs []blob
	for _, e := range tree.Tree {
		if e.Type != "blob" || !strings.HasSuffix(strings.ToLower(e.Path), ".md") {
			continue
		}
		rel, ok := relUnder(prefix, e.Path)
		if !ok {
			continue
		}
		if e.Size > maxFileSize {
			return nil, fmt.Errorf("%w: %s exceeds per-file size limit", ErrSourceUnavailable, e.Path)
		}
		blobs = append(blobs, blob{Rel: rel, SHA: e.SHA, Size: e.Size})
	}
	return validateCorpus(blobs)
}

// walkContents recursively enumerates markdown blobs via the contents API,
// used when the recursive tree is truncated.
func (s *Syncer) walkContents(ctx context.Context, commit, repoPath string) ([]blob, error) {
	u := fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", s.apiURL, s.src.Repo, repoPath, commit)
	var entries []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	}
	if err := s.getJSON(ctx, u, &entries); err != nil {
		return nil, err
	}
	var blobs []blob
	for _, e := range entries {
		switch e.Type {
		case "dir":
			sub, err := s.walkContents(ctx, commit, e.Path)
			if err != nil {
				return nil, err
			}
			blobs = append(blobs, sub...)
		case "file":
			if !strings.HasSuffix(strings.ToLower(e.Path), ".md") {
				continue
			}
			rel, ok := relUnder(s.src.Path, e.Path)
			if !ok {
				continue
			}
			if e.Size > maxFileSize {
				return nil, fmt.Errorf("%w: %s exceeds per-file size limit", ErrSourceUnavailable, e.Path)
			}
			blobs = append(blobs, blob{Rel: rel, SHA: e.SHA, Size: e.Size})
		}
	}
	return validateCorpus(blobs)
}

// download fetches raw file contents (pinned to commit) into the staging
// snapshot using bounded concurrency.
func (s *Syncer) download(ctx context.Context, commit string, blobs []blob, stage *staging) error {
	if len(blobs) == 0 {
		return nil
	}
	sem := make(chan struct{}, syncWorkers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, b := range blobs {
		wg.Add(1)
		go func(b blob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			mu.Lock()
			done := firstErr != nil
			mu.Unlock()
			if done {
				return
			}

			data, err := s.fetchRaw(ctx, commit, b.Rel)
			if err == nil {
				err = stage.writeFile(b.Rel, data)
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(b)
	}
	wg.Wait()
	if firstErr != nil {
		return fmt.Errorf("%w: %w", ErrSyncIncomplete, firstErr)
	}
	return nil
}

// fetchRaw downloads one file's content, pinned to the commit. By default it
// uses the authenticated GitHub contents API (Accept: raw) so private repos
// work and the token stays on the trusted api.github.com host. When an
// explicit raw base URL is configured (public repos / tests), it fetches from
// that host without a token.
func (s *Syncer) fetchRaw(ctx context.Context, commit, rel string) ([]byte, error) {
	full := rel
	if s.src.Path != "" {
		full = s.src.Path + "/" + rel
	}

	var u string
	useRawHost := strings.TrimSpace(s.rawURL) != ""
	if useRawHost {
		u = fmt.Sprintf("%s/%s/%s/%s", strings.TrimRight(s.rawURL, "/"), s.src.Repo, commit, full)
	} else {
		u = fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", s.apiURL, s.src.Repo, full, commit)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	if !useRawHost {
		req.Header.Set("Accept", "application/vnd.github.raw")
		// Token only ever goes to the trusted api.github.com host.
		if s.token != "" && isRealGitHubAPI(u) {
			req.Header.Set("Authorization", "Bearer "+s.token)
		}
	}
	resp, err := s.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: GET %s -> HTTP %d", ErrSourceUnavailable, u, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxFileSize {
		return nil, fmt.Errorf("%w: %s exceeds per-file size limit", ErrSourceUnavailable, rel)
	}
	return data, nil
}

// getJSON performs a GET against the GitHub API and decodes JSON. The optional
// token is applied only to the real api.github.com host.
func (s *Syncer) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if s.token != "" && isRealGitHubAPI(rawURL) {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.doer.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSourceUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s not found (HTTP 404)", ErrSourceUnavailable, rawURL)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%w: GET %s -> HTTP %d: %s", ErrSourceUnavailable, rawURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decode %s: %w", ErrSourceUnavailable, rawURL, err)
	}
	return nil
}

// touch refreshes checked_at on the live manifest without a new snapshot.
func (c *Cache) touch(now time.Time) {
	m, err := c.Load()
	if err != nil {
		return
	}
	m.CheckedAt = now
	snap, err := c.currentSnapshot()
	if err != nil {
		return
	}
	_ = writeManifest(path.Join(snap, "manifest.json"), m)
}

func isRealGitHubAPI(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), realAPIHost)
}

// relUnder returns the path relative to prefix and whether it is under it.
func relUnder(prefix, full string) (string, bool) {
	if prefix == "" {
		return full, true
	}
	p := prefix + "/"
	if !strings.HasPrefix(full, p) {
		return "", false
	}
	return strings.TrimPrefix(full, p), true
}

func validateCorpus(blobs []blob) ([]blob, error) {
	if len(blobs) > maxFiles {
		return nil, fmt.Errorf("%w: %d files exceeds limit %d", ErrSourceUnavailable, len(blobs), maxFiles)
	}
	var total int64
	for _, b := range blobs {
		total += b.Size
	}
	if total > maxTotalSize {
		return nil, fmt.Errorf("%w: corpus exceeds total size limit", ErrSourceUnavailable)
	}
	return blobs, nil
}
