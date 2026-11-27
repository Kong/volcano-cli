package cienv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVendor(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	assert.Empty(t, Vendor(env(nil)))
	assert.Equal(t, "generic", Vendor(env(map[string]string{"CI": "true"})))
	assert.Empty(t, Vendor(env(map[string]string{"CI": "false"})))
	assert.Equal(t, "gitlab", Vendor(env(map[string]string{"CI": "true", "GITLAB_CI": "true"})))
	assert.Equal(t, "buildkite", Vendor(env(map[string]string{"BUILDKITE": "true"})))
}
