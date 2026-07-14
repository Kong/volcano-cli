package docs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestSchemaVersion versions the on-disk manifest format.
const ManifestSchemaVersion = 1

// StaleAfter is how long a cache is served before it is reported stale. Reads
// never refresh automatically; staleness is informational only.
const StaleAfter = 7 * 24 * time.Hour

// FileEntry records one cached document and its GitHub blob SHA so a later sync
// can reuse unchanged files without re-downloading them.
type FileEntry struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

// Manifest describes a published cache snapshot.
type Manifest struct {
	SchemaVersion  int         `json:"schema_version"`
	Provider       string      `json:"provider"`
	Source         SourceRef   `json:"source"`
	ResolvedCommit string      `json:"resolved_commit"`
	Files          []FileEntry `json:"files"`
	SyncedAt       time.Time   `json:"synced_at"`
	CheckedAt      time.Time   `json:"checked_at"`
}

// Stale reports whether the snapshot is older than StaleAfter relative to now.
func (m *Manifest) Stale(now time.Time) bool {
	if m == nil || m.CheckedAt.IsZero() {
		return true
	}
	return now.Sub(m.CheckedAt) > StaleAfter
}

// pointer is the tiny, atomically-replaced file that names the live snapshot.
type pointer struct {
	Snapshot string `json:"snapshot"`
}

// Cache manages snapshot directories and the current pointer for one source.
type Cache struct {
	dir string // per-source cache dir
}

// NewCache returns a Cache rooted at the per-source directory.
func NewCache(dir string) *Cache { return &Cache{dir: dir} }

func (c *Cache) pointerPath() string  { return filepath.Join(c.dir, "current.json") }
func (c *Cache) snapshotsDir() string { return filepath.Join(c.dir, "snapshots") }
func (c *Cache) snapshotDir(n string) string {
	return filepath.Join(c.snapshotsDir(), n)
}

// Exists reports whether a published snapshot is available.
func (c *Cache) Exists() bool {
	_, err := c.currentSnapshot()
	return err == nil
}

func (c *Cache) currentSnapshot() (string, error) {
	data, err := os.ReadFile(c.pointerPath())
	if err != nil {
		return "", err
	}
	var p pointer
	if err := json.Unmarshal(data, &p); err != nil {
		return "", fmt.Errorf("corrupt cache pointer: %w", err)
	}
	if strings.TrimSpace(p.Snapshot) == "" {
		return "", errors.New("empty cache pointer")
	}
	dir := c.snapshotDir(p.Snapshot)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return "", fmt.Errorf("cache pointer references missing snapshot: %w", err)
	}
	return dir, nil
}

// Load reads the live manifest. Returns ErrCacheMissing when no snapshot exists.
func (c *Cache) Load() (*Manifest, error) {
	snap, err := c.currentSnapshot()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrCacheMissing
		}
		return nil, err
	}
	return readManifest(filepath.Join(snap, "manifest.json"))
}

// ReadFile returns the raw bytes of a cached document by its relative path.
func (c *Cache) ReadFile(relPath string) ([]byte, error) {
	snap, err := c.currentSnapshot()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrCacheMissing
		}
		return nil, err
	}
	clean, err := safeRelPath(relPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(snap, "files", clean))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrDocNotFound, relPath)
		}
		return nil, err
	}
	return data, nil
}

// staging represents an in-progress snapshot that is atomically published on
// success and discarded on failure, so an interrupted sync never corrupts the
// live cache.
type staging struct {
	cache *Cache
	name  string
	dir   string
}

func (c *Cache) newStaging(now time.Time) (*staging, error) {
	name := fmt.Sprintf("%d-%d", now.UTC().UnixNano(), os.Getpid())
	dir := c.snapshotDir(name)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create staging snapshot: %w", err)
	}
	return &staging{cache: c, name: name, dir: dir}, nil
}

// copyFrom copies a cached file (by relative path) from an existing snapshot
// into the staging snapshot, preserving reuse of unchanged blobs.
func (s *staging) copyFrom(src, relPath string) error {
	clean, err := safeRelPath(relPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(src, "files", clean))
	if err != nil {
		return err
	}
	return s.writeFile(relPath, data)
}

// writeFile writes one document into the staging snapshot.
func (s *staging) writeFile(relPath string, data []byte) error {
	clean, err := safeRelPath(relPath)
	if err != nil {
		return err
	}
	// clean is validated by safeRelPath and joined under the snapshot files
	// root, so it cannot escape the cache directory.
	dst := filepath.Join(s.dir, "files", clean)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600) //nolint:gosec // path sanitized by safeRelPath
}

// publish writes the manifest and atomically swings the current pointer to
// this snapshot, then prunes older snapshots.
func (s *staging) publish(m *Manifest) error {
	if err := writeManifest(filepath.Join(s.dir, "manifest.json"), m); err != nil {
		return err
	}
	if err := s.cache.writePointer(s.name); err != nil {
		return err
	}
	s.cache.prune(s.name)
	return nil
}

func (s *staging) discard() { _ = os.RemoveAll(s.dir) }

func (c *Cache) writePointer(name string) error {
	if err := os.MkdirAll(c.dir, 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(pointer{Snapshot: name})
	if err != nil {
		return err
	}
	tmp := c.pointerPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.pointerPath())
}

// prune removes every snapshot except keep to bound cache growth.
func (c *Cache) prune(keep string) {
	entries, err := os.ReadDir(c.snapshotsDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() == keep {
			continue
		}
		_ = os.RemoveAll(c.snapshotDir(e.Name()))
	}
}

func readManifest(p string) (*Manifest, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("corrupt manifest: %w", err)
	}
	return &m, nil
}

func writeManifest(p string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// safeRelPath validates a cache-relative path and returns its cleaned form.
// It rejects absolute paths and any traversal outside the files root.
func safeRelPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidID)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: absolute path %q", ErrInvalidID, rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes cache %q", ErrInvalidID, rel)
	}
	return clean, nil
}
