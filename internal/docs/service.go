package docs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/config"
)

const (
	defaultAPIURL = "https://api.github.com"
	defaultRawURL = "https://raw.githubusercontent.com"
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
}

// NewService resolves the documentation source and prepares the cache/syncer.
func NewService(opts Options) (*Service, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	src := ResolveSource(opts.Overrides, opts.Config, opts.Env)
	if err := src.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSource, err)
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
	rawURL := firstNonEmpty(opts.RawBaseURL, defaultRawURL)

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

// sections loads and parses the whole cached corpus.
func (s *Service) sections() ([]Section, error) {
	m, err := s.cache.Load()
	if err != nil {
		return nil, err
	}
	var all []Section
	for _, f := range m.Files {
		data, err := s.cache.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		all = append(all, ParseDoc(f.Path, data)...)
	}
	return all, nil
}

// Search bootstraps if needed then runs a BM25 query.
func (s *Service) Search(ctx context.Context, query, topic string, limit int, offline bool) ([]Result, error) {
	if err := s.ensureCache(ctx, offline); err != nil {
		return nil, err
	}
	secs, err := s.sections()
	if err != nil {
		return nil, err
	}
	return NewIndex(secs).Search(query, topic, limit), nil
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
	m, err := s.cache.Load()
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
		if data, err := s.cache.ReadFile(f.Path); err == nil {
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
	data, err := s.cache.ReadFile(docPath)
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
	if i := strings.IndexByte(id, '#'); i >= 0 {
		return id[:i], id[i+1:]
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
