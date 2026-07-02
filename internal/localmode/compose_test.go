package localmode

import (
	"os"
	"strings"
	"testing"

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
	for _, line := range strings.Split(template, "\n") {
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

	// Plan-limit env vars are required >0 for function/frontend deploy parity;
	// guard against an upstream regen that drops or renames them.
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
		"AWS_REGIONS",
	} {
		assert.Contains(t, template, key+":", "compose template missing required env var %s", key)
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
