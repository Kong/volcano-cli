package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The email domain allowlist and its enforcement mode are part of what config
// export writes, so a pulled manifest has to parse and push back unchanged.
func TestParseAuthSignupEmailDomains(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
auth:
  signup:
    enable_signup: true
    allowed_email_domains:
      - domain1.com
      - domain2.com
    allowed_email_domains_mode: signup_and_signin
`), noEnv)
	require.NoError(t, err)

	signup := manifest.Auth.Signup
	require.NotNil(t, signup)
	require.NotNil(t, signup.AllowedEmailDomains)
	assert.Equal(t, []string{"domain1.com", "domain2.com"}, *signup.AllowedEmailDomains)
	require.NotNil(t, signup.AllowedEmailDomainsMode)
	assert.Equal(t, "signup_and_signin", *signup.AllowedEmailDomainsMode)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	auth, ok := body["auth"].(map[string]any)
	require.True(t, ok)
	uploaded, ok := auth["signup"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"domain1.com", "domain2.com"}, uploaded["allowed_email_domains"])
	assert.Equal(t, "signup_and_signin", uploaded["allowed_email_domains_mode"])
}

// An exported manifest clears the list with an empty array and always names the
// mode; both must survive a push.
func TestParseAuthSignupEmailDomainsClearedList(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
auth:
  signup:
    allowed_email_domains: []
    allowed_email_domains_mode: disabled
`), noEnv)
	require.NoError(t, err)

	require.NotNil(t, manifest.Auth.Signup.AllowedEmailDomains)
	assert.Empty(t, *manifest.Auth.Signup.AllowedEmailDomains)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	auth, ok := body["auth"].(map[string]any)
	require.True(t, ok)
	uploaded, ok := auth["signup"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{}, uploaded["allowed_email_domains"],
		"an explicit empty list must stay explicit so the server clears the allowlist")
	assert.Equal(t, "disabled", uploaded["allowed_email_domains_mode"])
}
