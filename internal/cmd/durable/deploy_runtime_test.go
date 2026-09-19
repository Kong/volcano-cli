package durable

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clifunction "github.com/Kong/volcano-cli/internal/function"
)

func runtimeOption(name, extension string, isDefault, durable bool) apiclient.FunctionRuntimeOption {
	return apiclient.FunctionRuntimeOption{
		Name:           name,
		Default:        isDefault,
		DurableCapable: durable,
		Deployment: apiclient.FunctionRuntimeDeployment{
			FileExtensions: []string{extension},
		},
	}
}

func durableRuntimeCatalog() []apiclient.FunctionRuntimeOption {
	return []apiclient.FunctionRuntimeOption{
		runtimeOption("nodejs24.x", ".js", true, true),
		runtimeOption("python3.9", ".py", false, false),
		runtimeOption("python3.12", ".py", true, false),
		runtimeOption("python3.13", ".py", false, true),
		runtimeOption("python3.14", ".py", false, true),
		runtimeOption("ruby3.4", ".rb", true, false),
	}
}

// TestDurableRuntimeForUpgradesALanguageWhoseDefaultCannotRunDurable covers the
// only way a Python durable function can deploy from the CLI.
//
// Detection gives a source its language's default runtime, and Python's default
// is below the version durable execution needs. There is nowhere for the user to
// say otherwise — the durable deploy takes no runtime flag and does not read the
// manifest's — so without resolving it here the command refuses every Python
// source while the platform and the docs say it is supported.
func TestDurableRuntimeForUpgradesALanguageWhoseDefaultCannotRunDurable(t *testing.T) {
	t.Parallel()

	detected := clifunction.SourceInfo{
		Path:    "volcano/functions/order-pipeline.py",
		Name:    "order-pipeline",
		Runtime: runtimeOption("python3.12", ".py", true, false),
	}

	resolved, err := durableRuntimeFor(detected, durableRuntimeCatalog())
	require.NoError(t, err)
	assert.Equal(t, "python3.14", resolved.Runtime.Name,
		"the newest durable runtime for the language, compared by version rather than as text")
	assert.Equal(t, detected.Path, resolved.Path, "and nothing else about the source moves")
}

func TestDurableRuntimeForLeavesACapableRuntimeAlone(t *testing.T) {
	t.Parallel()

	detected := clifunction.SourceInfo{
		Path:    "volcano/functions/order-pipeline.js",
		Runtime: runtimeOption("nodejs24.x", ".js", true, true),
	}

	resolved, err := durableRuntimeFor(detected, durableRuntimeCatalog())
	require.NoError(t, err)
	assert.Equal(t, "nodejs24.x", resolved.Runtime.Name)
}

// A language with no durable runtime is still refused before anything is
// packaged, and the refusal names the file and what does work: the API's own
// answer arrives only after the archive has been built and sent, and cannot say
// which source the runtime was inferred from.
func TestDurableRuntimeForRefusesALanguageWithNoDurableRuntime(t *testing.T) {
	t.Parallel()

	detected := clifunction.SourceInfo{
		Path:    "volcano/functions/order-pipeline.rb",
		Runtime: runtimeOption("ruby3.4", ".rb", true, false),
	}

	_, err := durableRuntimeFor(detected, durableRuntimeCatalog())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "order-pipeline.rb")
	assert.Contains(t, err.Error(), "nodejs24.x")
	assert.NotContains(t, err.Error(), "upgrade",
		"there is nothing to upgrade to, so the message must not suggest one")
}

// Python's durable versions are two digits and its older ones are one, so a
// string comparison picks the wrong runtime.
func TestRuntimeVersionLessComparesNumbersNotText(t *testing.T) {
	t.Parallel()

	assert.True(t, runtimeVersionLess("python3.9", "python3.13"))
	assert.True(t, runtimeVersionLess("python3.13", "python3.14"))
	assert.False(t, runtimeVersionLess("python3.14", "python3.13"))
	assert.True(t, runtimeVersionLess("nodejs22.x", "nodejs24.x"))
}
