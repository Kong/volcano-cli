package localmode

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestComposeEnvironmentUsesDotEnvImageWhenProcessImageUnset(t *testing.T) {
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte("VOLCANO_IMAGE=kong/volcano:from-file\n"), 0o600))

	service := NewService(
		cliruntime.Deps{},
		WithEnvironment(func() []string { return []string{"PATH=/bin"} }, func(string) string { return "" }),
	)

	env, image, err := service.composeEnvironment()

	require.NoError(t, err)
	assert.Equal(t, "kong/volcano:from-file", image)
	actual, ok := lastEnvValue(env, "VOLCANO_IMAGE")
	require.True(t, ok)
	assert.Equal(t, "kong/volcano:from-file", actual)
}

func TestComposeEnvironmentPrefersProcessImageOverDotEnvImage(t *testing.T) {
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte("VOLCANO_IMAGE=kong/volcano:from-file\n"), 0o600))

	service := NewService(
		cliruntime.Deps{},
		WithEnvironment(func() []string { return []string{"PATH=/bin"} }, func(key string) string {
			if key == "VOLCANO_IMAGE" {
				return "kong/volcano:local-nightly"
			}
			return ""
		}),
	)

	env, image, err := service.composeEnvironment()

	require.NoError(t, err)
	assert.Equal(t, "kong/volcano:local-nightly", image)
	actual, ok := lastEnvValue(env, "VOLCANO_IMAGE")
	require.True(t, ok)
	assert.Equal(t, "kong/volcano:local-nightly", actual)
}

func TestComposeEnvironmentKeepsSingleResolvedVolcanoImage(t *testing.T) {
	withTempWorkingDir(t)
	require.NoError(t, os.WriteFile(".env.local", []byte("VOLCANO_IMAGE=kong/volcano:from-file\n"), 0o600))

	service := NewService(
		cliruntime.Deps{},
		WithEnvironment(func() []string {
			return []string{"PATH=/bin", "VOLCANO_IMAGE=kong/volcano:from-environ"}
		}, func(key string) string {
			if key == "VOLCANO_IMAGE" {
				return "kong/volcano:local-nightly"
			}
			return ""
		}),
	)

	env, image, err := service.composeEnvironment()

	require.NoError(t, err)
	assert.Equal(t, "kong/volcano:local-nightly", image)
	assert.Equal(t, []string{"kong/volcano:local-nightly"}, envValues(env, "VOLCANO_IMAGE"))
}

func TestComposeEnvironmentDefaultsVolcanoImage(t *testing.T) {
	withTempWorkingDir(t)

	service := NewService(
		cliruntime.Deps{},
		WithEnvironment(func() []string { return []string{"PATH=/bin"} }, func(string) string { return "" }),
	)

	env, image, err := service.composeEnvironment()

	require.NoError(t, err)
	assert.Equal(t, defaultVolcanoImage, image)
	actual, ok := lastEnvValue(env, "VOLCANO_IMAGE")
	require.True(t, ok)
	assert.Equal(t, defaultVolcanoImage, actual)
}

func TestDockerComposeTemplateLeavesServerOwnedLocalSecretsUnset(t *testing.T) {
	template := string(dockerComposeTemplate)

	// The local server generates and owns these secrets, so they must never
	// appear in the template at all.
	assert.NotContains(t, template, "JWT_SECRET:")
	assert.NotContains(t, template, "ENCRYPTION_KEY:")
	assert.NotContains(t, template, "SERVICE_KEY_SECRET:")

	// ANON_KEY_SECRET is an optional first-party-bootstrap passthrough. It may
	// appear only as an empty ${ANON_KEY_SECRET:-} default (which leaves it unset
	// so the server still owns it), never as a hardcoded secret value.
	for line := range strings.SplitSeq(template, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ANON_KEY_SECRET:") {
			assert.Equal(t, "ANON_KEY_SECRET: ${ANON_KEY_SECRET:-}", trimmed,
				"ANON_KEY_SECRET must only be an empty passthrough, not a hardcoded secret")
		}
	}
}

func TestDockerComposeTemplateExposesLocalFrontendProxy(t *testing.T) {
	template := string(dockerComposeTemplate)

	assert.Contains(t, template, `FRONTENDS_HTTP_PORT: "8080"`)
	assert.Contains(t, template, `FRONTEND_INVOCATION_DNS: "*.frontends.localhost"`)
	assert.Contains(t, template, `"8080:8080"`)
}

func TestDockerComposeTemplateSetsPlanLimitsAndRegions(t *testing.T) {
	template := string(dockerComposeTemplate)

	// Plan-limit env vars must survive an upstream template regen; guard against
	// one that drops or renames them. Presence only -- some are legitimately "0"
	// (unlimited/disabled), so this does not assert a value.
	for _, key := range []string{
		"FREE_FUNCTION_TIMEOUT",
		"FREE_FUNCTION_MEMORY",
		"FREE_FUNCTION_DISK",
		"PRO_FUNCTION_TIMEOUT",
		"PRO_FUNCTION_MEMORY",
		"PRO_FUNCTION_DISK",
		"FREE_FRONTEND_CUSTOM_DOMAINS",
		"PRO_FRONTEND_CUSTOM_DOMAINS",
		"FREE_SCHEDULER_COUNT",
		"PRO_SCHEDULER_COUNT",
		"FREE_BUILD_MAX_MINUTES",
		"PRO_BUILD_MAX_MINUTES",
		"FREE_LOG_RETENTION_DAYS",
		"PRO_LOG_RETENTION_DAYS",
		"FREE_IMAGE_OPTIMIZER_TIMEOUT",
		"PRO_IMAGE_OPTIMIZER_TIMEOUT",
		"FREE_IMAGE_OPTIMIZER_MEMORY",
		"PRO_IMAGE_OPTIMIZER_MEMORY",
		"FREE_IMAGE_OPTIMIZER_DISK",
		"PRO_IMAGE_OPTIMIZER_DISK",
		"FREE_DATABASE_CAP",
		"PRO_DATABASE_CAP",
		"FREE_DATABASE_STORAGE_LIMIT_MB",
		"PRO_DATABASE_STORAGE_LIMIT_MB",
		"FREE_STORAGE_BUCKETS_PER_PROJECT",
		"PRO_STORAGE_BUCKETS_PER_PROJECT",
		"AWS_REGIONS",
	} {
		assert.Contains(t, template, key+":", "compose template missing required env var %s", key)
	}
}

// TestDockerComposeTemplateUsesDurationStringsForTimingVars guards the
// duration-string contract with the server. The server parses these as Go
// time.Duration (RedisTimeout/UsageSyncInterval/UsageSyncLockTTL in
// cmd/server/internal/config), so a bare integer like "60" fails
// time.ParseDuration and the container refuses to start. Guard against an
// upstream regen that drops the duration suffix.
func TestDockerComposeTemplateUsesDurationStringsForTimingVars(t *testing.T) {
	template := string(dockerComposeTemplate)

	for _, key := range []string{
		"REDIS_TIMEOUT",
		"USAGE_SYNC_INTERVAL",
		"USAGE_SYNC_LOCK_TTL",
	} {
		value := composeTemplateEnvValue(t, template, key)
		_, err := time.ParseDuration(value)
		assert.NoErrorf(t, err, "%s must be a Go duration string (got %q); the server rejects bare integers", key, value)
	}
}

// composeTemplateEnvValue extracts the (optionally quoted) value assigned to key
// in the raw compose template, e.g. `REDIS_TIMEOUT: "60s"` -> "60s".
func composeTemplateEnvValue(t *testing.T, template, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*"?([^"\n]+)"?\s*$`)
	match := re.FindStringSubmatch(template)
	require.Len(t, match, 2, "compose template missing env var %s", key)
	return strings.TrimSpace(match[1])
}

func envValues(env []string, key string) []string {
	prefix := key + "="
	values := []string{}
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, prefix); ok {
			values = append(values, value)
		}
	}
	return values
}
