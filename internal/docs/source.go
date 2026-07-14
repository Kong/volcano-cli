// Package docs implements the `volcano docs` subsystem: fetching Volcano
// documentation from a configurable GitHub source into a local cache and
// searching it with a small, dependency-free BM25 index. It is designed
// primarily for AI agents that need to look up or validate facts against the
// official docs, with machine-readable (JSON) output as a first-class concern.
package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Kong/volcano-cli/internal/config"
)

// Provider identifies the backing documentation host. Only GitHub is
// supported today; the field keeps the cache key and JSON envelope forward
// compatible if another provider is ever added.
const Provider = "github"

// Compiled default source. Overridable via -ldflags -X if a release ever needs
// to retarget without a code change.
var (
	defaultRepo = "Kong/volcano-hosting"
	defaultRef  = "main"
	defaultPath = "docs"
)

// Environment overrides for the documentation source.
const (
	EnvRepo = "VOLCANO_DOCS_REPO"
	EnvRef  = "VOLCANO_DOCS_REF"
	EnvPath = "VOLCANO_DOCS_PATH"
)

// SourceRef locates a documentation corpus: a GitHub owner/name repository, a
// ref (branch, tag, or commit), and the subdirectory the docs live under.
type SourceRef struct {
	Repo string `json:"repository"`
	Ref  string `json:"ref"`
	Path string `json:"path"`
}

// DefaultSource returns the compiled-in default documentation source.
func DefaultSource() SourceRef {
	return SourceRef{Repo: defaultRepo, Ref: defaultRef, Path: defaultPath}
}

// Overrides carries explicit per-invocation source overrides, typically from
// --repo/--ref/--path flags. Empty fields are treated as "not set".
type Overrides struct {
	Repo string
	Ref  string
	Path string
}

// envLookup allows tests to stub environment reads without touching the real
// process environment. Nil uses os.LookupEnv.
type envLookup func(key string) (string, bool)

// ResolveSource applies the precedence flags > env (VOLCANO_DOCS_*) >
// config.DocsSource > compiled defaults, field by field. A missing value at
// one layer falls through to the next, so a user can override only the ref
// while inheriting the default repo and path.
func ResolveSource(ov Overrides, cfg *config.Config, lookup envLookup) SourceRef {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	src := DefaultSource()

	if cfg != nil && cfg.DocsSource != nil {
		if v := strings.TrimSpace(cfg.DocsSource.Repo); v != "" {
			src.Repo = v
		}
		if v := strings.TrimSpace(cfg.DocsSource.Ref); v != "" {
			src.Ref = v
		}
		if v := strings.TrimSpace(cfg.DocsSource.Path); v != "" {
			src.Path = v
		}
	}

	envOverride := !(cfg != nil && cfg.IgnoreEnv)
	if envOverride {
		if v, ok := lookup(EnvRepo); ok && strings.TrimSpace(v) != "" {
			src.Repo = strings.TrimSpace(v)
		}
		if v, ok := lookup(EnvRef); ok && strings.TrimSpace(v) != "" {
			src.Ref = strings.TrimSpace(v)
		}
		if v, ok := lookup(EnvPath); ok && strings.TrimSpace(v) != "" {
			src.Path = strings.TrimSpace(v)
		}
	}

	if v := strings.TrimSpace(ov.Repo); v != "" {
		src.Repo = v
	}
	if v := strings.TrimSpace(ov.Ref); v != "" {
		src.Ref = v
	}
	if v := strings.TrimSpace(ov.Path); v != "" {
		src.Path = v
	}

	src.Path = strings.Trim(strings.TrimSpace(src.Path), "/")
	return src
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Validate enforces the narrow GitHub owner/name + ref + path shape. Rejecting
// anything else keeps configurable sources from becoming a path-traversal or
// SSRF vector: we only ever talk to api.github.com / raw.githubusercontent.com
// for a well-formed repository.
func (s SourceRef) Validate() error {
	if !repoPattern.MatchString(s.Repo) {
		return fmt.Errorf("invalid docs repository %q: expected GitHub owner/name", s.Repo)
	}
	if strings.TrimSpace(s.Ref) == "" {
		return fmt.Errorf("invalid docs ref: must not be empty")
	}
	if strings.ContainsAny(s.Ref, " \t\n") {
		return fmt.Errorf("invalid docs ref %q", s.Ref)
	}
	clean := path.Clean("/" + s.Path)
	if strings.Contains(s.Path, "..") || clean == "/" && s.Path != "" {
		return fmt.Errorf("invalid docs path %q", s.Path)
	}
	return nil
}

// CacheKey derives a stable, filesystem-safe identifier for a source so that
// distinct repo/ref/path combinations never share a cache namespace.
func (s SourceRef) CacheKey() string {
	sum := sha256.Sum256([]byte(Provider + "|" + s.Repo + "|" + s.Ref + "|" + s.Path))
	return hex.EncodeToString(sum[:])[:32]
}

// cacheVersion namespaces the on-disk layout so a future breaking change to the
// manifest/snapshot format can bump it without colliding with old caches.
const cacheVersion = "v1"

// baseCacheDir returns the root docs cache directory, honoring an injected
// override for tests and falling back to os.UserCacheDir()/volcano.
func baseCacheDir(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user cache dir: %w", err)
	}
	return filepath.Join(dir, "volcano"), nil
}

// CacheDir returns the per-source cache directory:
// <base>/docs/<version>/<source-key>.
func CacheDir(override string, src SourceRef) (string, error) {
	base, err := baseCacheDir(override)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "docs", cacheVersion, src.CacheKey()), nil
}
