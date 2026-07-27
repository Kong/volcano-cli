package localmode

import (
	"os"
	"regexp"
	"slices"
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

// TestDefaultVolcanoImageIsPublishedLocalTag guards that the built-in default
// points at the local-mode image family (kong/volcano:local-*) that
// volcano-hosting publishes and that `volcano start` must run — never the empty
// string, a non-local tag, or the private cloud image.
func TestDefaultVolcanoImageIsPublishedLocalTag(t *testing.T) {
	assert.Truef(t, strings.HasPrefix(defaultVolcanoImage, "kong/volcano:local-"),
		"default local-mode image must be a kong/volcano:local-* tag (got %q)", defaultVolcanoImage)
}

func TestDropIncompleteFirstPartyBootstrap(t *testing.T) {
	// A partial set (the common case: a dev keeps the anon key / device client id
	// in .env.local for signup/login) is stripped so the local server skips
	// bootstrap and auto-provisions instead of hanging on startup.
	partial := []string{
		"PATH=/bin",
		"VOLCANO_FIRST_PARTY_ANON_KEY=ak-123",
		"ANON_KEY_SECRET=shh",
		"VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID=volcano-cli",
	}
	got := dropIncompleteFirstPartyBootstrap(append([]string{}, partial...))
	for _, k := range append(slices.Clone(firstPartyBootstrapKeys), "ANON_KEY_SECRET") {
		_, ok := lastEnvValue(got, k)
		assert.Falsef(t, ok, "%s should be stripped from a partial first-party set", k)
	}
	assert.Contains(t, got, "PATH=/bin", "unrelated env must be preserved")

	// The complete set is preserved so deliberate first-party bootstrap still works.
	complete := []string{"PATH=/bin"}
	for _, k := range firstPartyBootstrapKeys {
		complete = append(complete, k+"=x")
	}
	kept := dropIncompleteFirstPartyBootstrap(append([]string{}, complete...))
	for _, k := range firstPartyBootstrapKeys {
		v, ok := lastEnvValue(kept, k)
		assert.Truef(t, ok && v == "x", "%s should be kept when the full set is present", k)
	}

	// An empty value counts as absent, so a set with one blank var is still partial.
	blank := slices.Clone(complete)
	blank = withoutEnvKey(blank, "VOLCANO_FIRST_PARTY_USER_TOKEN")
	blank = append(blank, "VOLCANO_FIRST_PARTY_USER_TOKEN=")
	stripped := dropIncompleteFirstPartyBootstrap(append([]string{}, blank...))
	_, ok := lastEnvValue(stripped, "VOLCANO_FIRST_PARTY_ANON_KEY")
	assert.False(t, ok, "a set with one empty var is partial and must be stripped")
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

func TestDockerComposePersistencePreservesFunctionSource(t *testing.T) {
	template := string(dockerComposePersistence)

	assert.Contains(t, template, "volcano-functions:/app/local-functions")
	assert.Contains(t, template, "volcano-functions:")
}

func TestDockerComposeTemplateSetsRegionsButNotPlanLimits(t *testing.T) {
	template := string(dockerComposeTemplate)

	assert.Contains(t, template, "AWS_REGIONS:", "compose template must still advertise local deployable regions")

	// Local plan defaults are baked into the server binary so the distributed CLI
	// template does not leak FREE_*/PRO_* entitlement numbers. The hosting config
	// package has a reflection guard that fails when a new plan env var is added
	// without a baked local default.
	planEnv := regexp.MustCompile(`(?m)^\s*(FREE|PRO)_[A-Z0-9_]+:`)
	assert.Empty(t, planEnv.FindString(template), "local-mode template must not ship FREE_*/PRO_* plan-limit env vars")
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

// TestDockerComposeTemplateHasNoProductionIdentifiers fails if the shipped
// local-mode template carries any production/staging identifier. The template is
// generated verbatim from the hosting source with no sanitization pass, so this
// is the defense-in-depth guard on the artifact that actually ships in the CLI.
// The template is controlled config (no legitimate examples), so a strict
// denylist is safe.
func TestDockerComposeTemplateHasNoProductionIdentifiers(t *testing.T) {
	template := string(dockerComposeTemplate)

	forbidden := map[string]*regexp.Regexp{
		"AWS ARN":              regexp.MustCompile(`arn:aws:`),
		"12-digit AWS account": regexp.MustCompile(`\b\d{12}\b`),
		"volcano.dev domain":   regexp.MustCompile(`\bvolcano\.dev\b`),
		"AWS access key id":    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	}
	for label, re := range forbidden {
		match := re.FindString(template)
		assert.Emptyf(t, match, "local-mode template leaks %s (%q); it must not carry production identifiers", label, match)
	}

	// AWS credentials must be the inert "local" placeholder, never a real key.
	for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*"?([^"\n]+?)"?\s*$`)
		for _, match := range re.FindAllStringSubmatch(template, -1) {
			assert.Equalf(t, "local", strings.TrimSpace(match[1]), "%s must be the inert local placeholder", key)
		}
	}
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
