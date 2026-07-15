package docs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Kong/volcano-cli/internal/config"
)

const (
	defaultAPIURL = "https://api.github.com"
	httpTimeout   = 30 * time.Second
)

// Options configures a Service. The command layer builds this from
// cliruntime.Deps, the loaded config, and --repo/--ref/--path flags.
type Options struct {
	Overrides    Overrides
	Config       *config.Config
	HTTPClient   HTTPDoer
	CacheDir     string
	GitHubAPIURL string
	RawBaseURL   string
	Token        string
	Now          func() time.Time
	Env          func(string) (string, bool)
}

// Service is the entry point for docs operations, bundling a resolved source,
// its cache, and a syncer.
type Service struct {
	src    SourceRef
	cache  *Cache
	syncer *Syncer
	now    func() time.Time

	// Resident index cache. In one-shot CLI use each process builds it once;
	// in the long-lived MCP server it is reused across tool calls and rebuilt
	// only when the published cache snapshot generation changes. Guarded by mu.
	mu        sync.Mutex
	idx       *Index
	idxGen    string
	idxBuilds int // test-observable: number of index (re)builds
}

// NewService resolves the documentation source and prepares the cache/syncer.
func NewService(opts Options) (*Service, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	src := ResolveSource(opts.Overrides, opts.Config, opts.Env)
	if err := src.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSource, err)
	}
	cacheDir, err := CacheDir(opts.CacheDir, src)
	if err != nil {
		return nil, err
	}
	cache := NewCache(cacheDir)

	doer := opts.HTTPClient
	if doer == nil {
		doer = &http.Client{Timeout: httpTimeout}
	}
	apiURL := strings.TrimRight(firstNonEmpty(opts.GitHubAPIURL, defaultAPIURL), "/")
	// rawURL is intentionally empty by default: downloads then go through the
	// authenticated contents API (works for private repos). Set RawBaseURL only
	// for public-repo raw-host downloads or tests.
	rawURL := strings.TrimSpace(opts.RawBaseURL)

	return &Service{
		src:   src,
		cache: cache,
		now:   now,
		syncer: &Syncer{
			doer:   doer,
			apiURL: apiURL,
			rawURL: rawURL,
			token:  strings.TrimSpace(opts.Token),
			now:    now,
			cache:  cache,
			src:    src,
		},
	}, nil
}

// Source returns the resolved documentation source.
func (s *Service) Source() SourceRef { return s.src }

// CacheState reports the current cache metadata for the JSON envelope.
type CacheState struct {
	Key       string     `json:"key"`
	SyncedAt  *time.Time `json:"synced_at"`
	CheckedAt *time.Time `json:"checked_at"`
	Stale     bool       `json:"stale"`
	Offline   bool       `json:"offline"`
}

// CacheState builds cache metadata; manifest may be nil when no cache exists.
func (s *Service) CacheState(offline bool) CacheState {
	st := CacheState{Key: s.src.CacheKey(), Offline: offline, Stale: true}
	if m, err := s.cache.Load(); err == nil {
		st.SyncedAt = &m.SyncedAt
		st.CheckedAt = &m.CheckedAt
		st.Stale = m.Stale(s.now())
	}
	return st
}

// ResolvedCommit returns the live cache's pinned commit, or "".
func (s *Service) ResolvedCommit() string {
	if m, err := s.cache.Load(); err == nil {
		return m.ResolvedCommit
	}
	return ""
}

// Sync fetches/refreshes the cache from the source.
func (s *Service) Sync(ctx context.Context, force bool) (*SyncResult, error) {
	return s.syncer.Sync(ctx, force)
}

// ensureCache bootstraps the cache exactly once when it is missing, unless
// offline. It never refreshes an existing (even stale) cache.
func (s *Service) ensureCache(ctx context.Context, offline bool) error {
	if s.cache.Exists() {
		return nil
	}
	if offline {
		return ErrCacheMissing
	}
	if _, err := s.syncer.Sync(ctx, false); err != nil {
		return err
	}
	return nil
}

// sections parses the whole cached corpus from a single pinned snapshot, so the
// manifest file list and the file contents are guaranteed to come from the same
// generation. It returns the snapshot name so the caller can detect an external
// republish that raced the read.
func (s *Service) sections() ([]Section, string, error) {
	snap, err := s.cache.openSnapshot()
	if err != nil {
		return nil, "", err
	}
	m, err := snap.manifest()
	if err != nil {
		return nil, snap.name, err
	}
	var all []Section
	for _, f := range m.Files {
		data, err := snap.readFile(f.Path)
		if err != nil {
			return nil, snap.name, err
		}
		all = append(all, ParseDoc(f.Path, data)...)
	}
	return all, snap.name, nil
}

// Search bootstraps if needed then runs a BM25 query.
func (s *Service) Search(ctx context.Context, query, topic string, limit int, offline bool) ([]Result, error) {
	idx, err := s.resolveIndex(ctx, offline)
	if err != nil {
		return nil, err
	}
	return idx.Search(query, topic, limit), nil
}

// resolveIndex returns a BM25 index for the current cache snapshot, reusing a
// previously built one while the published snapshot generation is unchanged.
// It bootstraps the cache once (unless offline). MCP stdio requests are
// processed sequentially, so a mutex is sufficient; a generation recheck after
// building guards against an external `docs sync` publishing mid-build.
func (s *Service) resolveIndex(ctx context.Context, offline bool) (*Index, error) {
	if err := s.ensureCache(ctx, offline); err != nil {
		return nil, err
	}

	gen, err := s.cache.currentSnapshotName()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if s.idx != nil && s.idxGen == gen {
		idx := s.idx
		s.mu.Unlock()
		return idx, nil
	}
	s.mu.Unlock()

	// Build outside the lock from a pinned snapshot. If an external sync
	// republishes/prunes underneath us, retry against the new snapshot instead
	// of surfacing a transient read error.
	const maxAttempts = 3
	var lastErr error
	for range maxAttempts {
		secs, builtGen, err := s.sections()
		if err != nil {
			lastErr = err
			if cur, cerr := s.cache.currentSnapshotName(); cerr == nil && cur != gen {
				gen = cur
				continue
			}
			return nil, err
		}
		if after, err := s.cache.currentSnapshotName(); err != nil {
			return nil, err
		} else if after != builtGen {
			gen = after
			continue
		}

		idx := NewIndex(secs)
		s.mu.Lock()
		s.idx = idx
		s.idxGen = builtGen
		s.idxBuilds++
		s.mu.Unlock()
		return idx, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("%w: cache changed repeatedly during index build", ErrSyncIncomplete)
}

// ListItem is one document entry for `docs list`.
type ListItem struct {
	Path  string `json:"path"`
	Topic string `json:"topic"`
	Title string `json:"title"`
}

// List returns the cached documents, optionally filtered by topic.
func (s *Service) List(ctx context.Context, topic string, offline bool) ([]ListItem, error) {
	if err := s.ensureCache(ctx, offline); err != nil {
		return nil, err
	}
	snap, err := s.cache.openSnapshot()
	if err != nil {
		return nil, err
	}
	m, err := snap.manifest()
	if err != nil {
		return nil, err
	}
	var out []ListItem
	for _, f := range m.Files {
		t := topicOf(f.Path)
		if topic != "" && !strings.EqualFold(t, topic) {
			continue
		}
		title := f.Path
		if data, err := snap.readFile(f.Path); err == nil {
			title = deriveTitle(f.Path, strings.Split(string(data), "\n"))
		}
		out = append(out, ListItem{Path: f.Path, Topic: t, Title: title})
	}
	return out, nil
}

// GetResult is the payload for `docs get`.
type GetResult struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Topic       string   `json:"topic"`
	Title       string   `json:"title"`
	Anchor      string   `json:"anchor,omitempty"`
	HeadingPath []string `json:"heading_path,omitempty"`
	LineStart   int      `json:"line_start,omitempty"`
	LineEnd     int      `json:"line_end,omitempty"`
	Content     string   `json:"content"`
}

// Get retrieves a document by path, or a single section when the id carries a
// #anchor. A bare path returns the complete raw markdown document.
func (s *Service) Get(ctx context.Context, id string, offline bool) (*GetResult, error) {
	if err := s.ensureCache(ctx, offline); err != nil {
		return nil, err
	}
	docPath, anchor := splitID(id)
	if strings.TrimSpace(docPath) == "" {
		return nil, fmt.Errorf("%w: empty id", ErrInvalidID)
	}
	// Read through a pinned snapshot (consistent with sections()/List) so a
	// concurrent external sync + prune can't spuriously 404 a doc that exists.
	snap, err := s.cache.openSnapshot()
	if err != nil {
		return nil, err
	}
	data, err := snap.readFile(docPath)
	if err != nil {
		return nil, err
	}
	title := deriveTitle(docPath, strings.Split(string(data), "\n"))
	if anchor == "" {
		return &GetResult{
			ID:      docPath,
			Path:    docPath,
			Topic:   topicOf(docPath),
			Title:   title,
			Content: string(data),
		}, nil
	}
	for _, sec := range ParseDoc(docPath, data) {
		if sec.Anchor == anchor {
			return &GetResult{
				ID:          sec.ID(),
				Path:        docPath,
				Topic:       sec.Topic,
				Title:       title,
				Anchor:      anchor,
				HeadingPath: sec.HeadingPath,
				LineStart:   sec.LineStart,
				LineEnd:     sec.LineEnd,
				Content:     sec.Body,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrDocNotFound, id)
}

func splitID(id string) (docPath, anchor string) {
	id = strings.TrimSpace(id)
	if p, a, ok := strings.Cut(id, "#"); ok {
		return p, a
	}
	return id, ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
