package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
)

func TestResolveSourceDefaults(t *testing.T) {
	src := ResolveSource(Overrides{}, nil, emptyEnv)
	assert.Equal(t, DefaultSource(), src)
}

func TestResolveSourcePrecedence(t *testing.T) {
	cfg := &config.Config{DocsSource: &config.DocsSourceConfig{Repo: "cfg/repo", Ref: "cfg-ref"}}
	env := func(key string) (string, bool) {
		if key == EnvRef {
			return "env-ref", true
		}
		return "", false
	}
	// flag ref beats env ref beats config ref; repo falls through to config;
	// path falls through to the compiled default.
	src := ResolveSource(Overrides{Ref: "flag-ref"}, cfg, env)
	assert.Equal(t, "cfg/repo", src.Repo)
	assert.Equal(t, "flag-ref", src.Ref)
	assert.Equal(t, defaultPath, src.Path)
}

func TestResolveSourceEnvOverConfig(t *testing.T) {
	cfg := &config.Config{DocsSource: &config.DocsSourceConfig{Ref: "cfg-ref"}}
	env := func(key string) (string, bool) {
		if key == EnvRef {
			return "env-ref", true
		}
		return "", false
	}
	src := ResolveSource(Overrides{}, cfg, env)
	assert.Equal(t, "env-ref", src.Ref)
}

func TestResolveSourceIgnoreEnv(t *testing.T) {
	cfg := &config.Config{IgnoreEnv: true}
	env := func(string) (string, bool) { return "should-not-apply", true }
	src := ResolveSource(Overrides{}, cfg, env)
	assert.Equal(t, DefaultSource(), src)
}

func TestSourceValidate(t *testing.T) {
	require.NoError(t, SourceRef{Repo: "Kong/volcano-hosting", Ref: "main", Path: "docs"}.Validate())
	require.Error(t, SourceRef{Repo: "not-a-repo", Ref: "main"}.Validate())
	require.Error(t, SourceRef{Repo: "a/b", Ref: ""}.Validate())
	require.Error(t, SourceRef{Repo: "a/b", Ref: "main", Path: "../etc"}.Validate())
}

func TestCacheKeyDistinctPerSource(t *testing.T) {
	a := SourceRef{Repo: "a/b", Ref: "main", Path: "docs"}.CacheKey()
	b := SourceRef{Repo: "a/b", Ref: "next", Path: "docs"}.CacheKey()
	assert.NotEqual(t, a, b)
	assert.Len(t, a, 32)
}

func emptyEnv(string) (string, bool) { return "", false }
