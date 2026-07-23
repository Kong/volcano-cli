package docs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGitHub is a minimal in-memory GitHub API stand-in for docs sync tests.
type fakeGitHub struct {
	repo      string
	docsPath  string
	commit    string
	files     map[string]string // full repo path (docs/...) -> content
	truncated bool

	server      *httptest.Server
	apiHits     atomic.Int64
	rawHits     atomic.Int64
	rawAuthSeen atomic.Bool     // set if a raw-host request carried an Authorization header
	failRaw     map[string]bool // full repo paths whose raw download should 500
}

func blobSHA(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:])
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		repo:     "Kong/volcano-hosting",
		docsPath: "docs",
		commit:   "commit-sha-1",
		files:    map[string]string{},
		failRaw:  map[string]bool{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) add(rel, content string) {
	f.files[f.docsPath+"/"+rel] = content
}

func (f *fakeGitHub) handle(w http.ResponseWriter, r *http.Request) {
	f.apiHits.Add(1)
	base := "/repos/" + f.repo
	switch {
	case strings.HasPrefix(r.URL.Path, base+"/commits/"):
		writeJSONResp(w, map[string]string{"sha": f.commit})
	case strings.HasPrefix(r.URL.Path, base+"/git/trees/"):
		f.handleTree(w)
	case strings.HasPrefix(r.URL.Path, base+"/contents/"):
		full := strings.TrimPrefix(r.URL.Path, base+"/contents/")
		if strings.Contains(r.Header.Get("Accept"), "raw") {
			f.handleRawFile(w, full)
			return
		}
		f.handleContentsDir(w, full)
	case strings.HasPrefix(r.URL.Path, "/"+f.repo+"/"):
		// Raw-host download: /{owner}/{name}/{commit}/{full}.
		f.handleRawHost(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (f *fakeGitHub) handleRawHost(w http.ResponseWriter, r *http.Request) {
	f.rawHits.Add(1)
	if r.Header.Get("Authorization") != "" {
		f.rawAuthSeen.Store(true)
	}
	rest := strings.TrimPrefix(r.URL.Path, "/"+f.repo+"/")
	_, full, ok := strings.Cut(rest, "/") // drop the commit segment
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	content, ok := f.files[full]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(content))
}

func (f *fakeGitHub) handleTree(w http.ResponseWriter) {
	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	}
	out := struct {
		Tree      []entry `json:"tree"`
		Truncated bool    `json:"truncated"`
	}{Truncated: f.truncated}
	if !f.truncated {
		for full, content := range f.files {
			out.Tree = append(out.Tree, entry{Path: full, Type: "blob", SHA: blobSHA(content), Size: int64(len(content))})
		}
	}
	writeJSONResp(w, out)
}

func (f *fakeGitHub) handleRawFile(w http.ResponseWriter, full string) {
	f.rawHits.Add(1)
	if f.failRaw[full] {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	content, ok := f.files[full]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, _ = w.Write([]byte(content))
}

func (f *fakeGitHub) handleContentsDir(w http.ResponseWriter, dir string) {
	type entry struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
		Size int64  `json:"size"`
	}
	children := map[string]entry{}
	prefix := dir + "/"
	for full, content := range f.files {
		if !strings.HasPrefix(full, prefix) {
			continue
		}
		rest := strings.TrimPrefix(full, prefix)
		if seg, _, ok := strings.Cut(rest, "/"); ok {
			subdir := dir + "/" + seg
			children[subdir] = entry{Path: subdir, Type: "dir"}
		} else {
			children[full] = entry{Path: full, Type: "file", SHA: blobSHA(content), Size: int64(len(content))}
		}
	}
	var list []entry
	for _, e := range children {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	writeJSONResp(w, list)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGitHub) newService(t *testing.T, cacheDir string, now func() time.Time) *Service {
	t.Helper()
	svc, err := NewService(Options{
		Overrides:    Overrides{Repo: f.repo, Ref: "main", Path: f.docsPath},
		HTTPClient:   f.server.Client(),
		CacheDir:     cacheDir,
		GitHubAPIURL: f.server.URL,
		Now:          now,
		Env:          emptyEnv,
	})
	require.NoError(t, err)
	return svc
}

func TestSyncFetchesAndCaches(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("auth/overview.md", "# Overview\ncontent")
	f.add("auth/keys.md", "# Keys\nservice_role details")

	svc := f.newService(t, t.TempDir(), nil)
	res, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Added)
	assert.True(t, res.Changed)
	assert.Equal(t, "commit-sha-1", res.ResolvedCommit)

	data, err := svc.Get(context.Background(), "auth/keys.md", true)
	require.NoError(t, err)
	assert.Contains(t, data.Content, "service_role")
}

func TestSyncReusesUnchangedBlobs(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A\nalpha")
	f.add("b.md", "# B\nbeta")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	// Change one file and bump the commit; only the changed file re-downloads.
	f.files[f.docsPath+"/b.md"] = "# B\nbeta updated"
	f.commit = "commit-sha-2"
	rawBefore := f.rawHits.Load()

	svc2 := f.newService(t, cache, nil)
	res, err := svc2.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Updated)
	assert.Equal(t, 1, res.Unchanged)
	assert.Equal(t, int64(1), f.rawHits.Load()-rawBefore)
}

func TestSyncNoChangeUpdatesCheckedAtOnly(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A\nalpha")
	cache := t.TempDir()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := f.newService(t, cache, func() time.Time { return t0 })
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	t1 := t0.Add(48 * time.Hour)
	svc2 := f.newService(t, cache, func() time.Time { return t1 })
	res, err := svc2.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.False(t, res.Changed)

	m, err := NewCache(mustCacheDir(t, cache, f)).Load()
	require.NoError(t, err)
	assert.Equal(t, t0, m.SyncedAt.UTC())
	assert.Equal(t, t1, m.CheckedAt.UTC())
}

func TestSyncRemovesDeletedFiles(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A")
	f.add("b.md", "# B")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	delete(f.files, f.docsPath+"/b.md")
	f.commit = "commit-sha-2"
	svc2 := f.newService(t, cache, nil)
	res, err := svc2.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Removed)

	_, err = svc2.Get(context.Background(), "b.md", true)
	assert.ErrorIs(t, err, ErrDocNotFound)
}

func TestSyncTruncatedTreeFallsBackToWalk(t *testing.T) {
	f := newFakeGitHub(t)
	f.truncated = true
	f.add("top.md", "# Top")
	f.add("sub/deep.md", "# Deep\ncontent")

	svc := f.newService(t, t.TempDir(), nil)
	res, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Added)

	got, err := svc.Get(context.Background(), "sub/deep.md", true)
	require.NoError(t, err)
	assert.Contains(t, got.Content, "Deep")
}

func TestSyncPartialFailurePreservesPreviousCache(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A\nalpha")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	// New commit adds a file whose download fails; the sync must abort and
	// leave the previous snapshot intact.
	f.add("b.md", "# B\nbeta")
	f.failRaw[f.docsPath+"/b.md"] = true
	f.commit = "commit-sha-2"

	svc2 := f.newService(t, cache, nil)
	_, err = svc2.Sync(context.Background(), false)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSyncIncomplete)

	// Previous doc still served; the failed new doc is absent.
	_, err = svc2.Get(context.Background(), "a.md", true)
	require.NoError(t, err)
	m, err := NewCache(mustCacheDir(t, cache, f)).Load()
	require.NoError(t, err)
	assert.Equal(t, "commit-sha-1", m.ResolvedCommit)
}

func TestReadOfflineWithoutCacheFails(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A")
	svc := f.newService(t, t.TempDir(), nil)
	_, err := svc.Search(context.Background(), "a", "", 5, true)
	require.ErrorIs(t, err, ErrCacheMissing)
	assert.Equal(t, int64(0), f.apiHits.Load())
}

func TestReadBootstrapsOnceThenNoNetwork(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# Alpha\nsearchable content")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Search(context.Background(), "searchable", "", 5, false)
	require.NoError(t, err)
	afterBootstrap := f.apiHits.Load()
	assert.Positive(t, afterBootstrap)

	// Second read with an existing cache makes no network calls.
	svc2 := f.newService(t, cache, nil)
	_, err = svc2.Search(context.Background(), "searchable", "", 5, false)
	require.NoError(t, err)
	assert.Equal(t, afterBootstrap, f.apiHits.Load())
}

func TestStaleCacheReported(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A")
	cache := t.TempDir()

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := f.newService(t, cache, func() time.Time { return t0 })
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	later := t0.Add(StaleAfter + time.Hour)
	svc2 := f.newService(t, cache, func() time.Time { return later })
	cs := svc2.CacheState(false)
	assert.True(t, cs.Stale)
}

func TestInvalidSourceRejected(t *testing.T) {
	_, err := NewService(Options{
		Overrides: Overrides{Repo: "not-a-repo", Ref: "main"},
		Env:       emptyEnv,
	})
	assert.ErrorIs(t, err, ErrInvalidSource)
}

func mustCacheDir(t *testing.T, base string, f *fakeGitHub) string {
	t.Helper()
	dir, err := CacheDir(base, SourceRef{Repo: f.repo, Ref: "main", Path: f.docsPath})
	require.NoError(t, err)
	return dir
}

func TestResidentIndexReuseAndInvalidation(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# Alpha\nsearchable alpha content")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)

	// Two searches on the same service build the index once.
	_, err = svc.Search(context.Background(), "searchable", "", 5, true)
	require.NoError(t, err)
	_, err = svc.Search(context.Background(), "alpha", "", 5, true)
	require.NoError(t, err)
	assert.Equal(t, 1, svc.idxBuilds, "index should be built once and reused")

	// A new published snapshot (force sync) invalidates the resident index.
	_, err = svc.Sync(context.Background(), true)
	require.NoError(t, err)
	_, err = svc.Search(context.Background(), "searchable", "", 5, true)
	require.NoError(t, err)
	assert.Equal(t, 2, svc.idxBuilds, "index should rebuild after snapshot generation changes")
}

func TestSyncForceRedownloadsAll(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A")
	f.add("b.md", "# B")
	cache := t.TempDir()

	svc := f.newService(t, cache, nil)
	_, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)
	rawAfterFirst := f.rawHits.Load()

	// Same commit + --force must re-download every file (no SHA reuse).
	svc2 := f.newService(t, cache, nil)
	res, err := svc2.Sync(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Unchanged)
	assert.Equal(t, int64(2), f.rawHits.Load()-rawAfterFirst)
}

func TestSyncDownloadsViaRawHostWithoutToken(t *testing.T) {
	f := newFakeGitHub(t)
	f.add("a.md", "# A\nalpha")
	f.add("sub/b.md", "# B\nbeta")

	// Setting RawBaseURL selects the raw-host download path. A token is
	// configured to prove it is never sent to the raw host.
	svc, err := NewService(Options{
		Overrides:    Overrides{Repo: f.repo, Ref: "main", Path: f.docsPath},
		HTTPClient:   f.server.Client(),
		CacheDir:     t.TempDir(),
		GitHubAPIURL: f.server.URL,
		RawBaseURL:   f.server.URL,
		Token:        "secret-token",
		Env:          emptyEnv,
	})
	require.NoError(t, err)

	res, err := svc.Sync(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, res.Added)
	assert.Positive(t, f.rawHits.Load(), "raw-host download path should be used")
	assert.False(t, f.rawAuthSeen.Load(), "token must never be sent to the raw host")

	got, err := svc.Get(context.Background(), "sub/b.md", true)
	require.NoError(t, err)
	assert.Contains(t, got.Content, "beta")
}

func TestRawHostSelection(t *testing.T) {
	// Explicit override always wins.
	assert.Equal(t, "http://x", (&Syncer{rawURL: "http://x", apiURL: defaultAPIURL}).rawHost())
	// No token + real GitHub API → raw CDN (avoids the unauth rate limit).
	assert.Equal(t, defaultRawURL, (&Syncer{apiURL: defaultAPIURL}).rawHost())
	// Token present → authenticated contents API (empty raw host).
	assert.Empty(t, (&Syncer{apiURL: defaultAPIURL, token: "t"}).rawHost())
	// Injected (test) API host → contents API, keeps tests hermetic.
	assert.Empty(t, (&Syncer{apiURL: "http://127.0.0.1:1234"}).rawHost())
}
