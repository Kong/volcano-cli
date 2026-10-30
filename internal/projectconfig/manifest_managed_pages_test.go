package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pulled manifest carries the managed pages' appearance and every hosted page
// type the server exports; deploying it again must upload them unchanged.
func TestParseManagedPagesAppearanceAndPageTypes(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
auth:
  managed_pages:
    enabled: true
    pages:
      login:
        html: '<html>login</html>'
        css: 'body{}'
      signup:
        html: '<html>signup</html>'
      forgot_password:
        html: '<html>forgot</html>'
      device:
        html: '<html>device</html>'
      verify_email:
        html: '<html>verify</html>'
      reset_password:
        html: '<html>reset</html>'
    appearance:
      theme:
        version: 1
        colors:
          background: '#f7f5f2'
          surface: '#ffffff'
          text: '#1a1a1a'
          accent: '#1d4ed8'
          accent_text: '#ffffff'
        font: humanist
        scale: large
        density: spacious
        radius: large
      layouts:
        login: split-left
        signup: split-right
        forgot_password: centered
        device: split-left
        verify_email: centered
        reset_password: split-right
`), noEnv)
	require.NoError(t, err)

	pages := manifest.Auth.ManagedPages.Pages
	require.NotNil(t, pages.Signup)
	assert.Equal(t, "<html>signup</html>", pages.Signup.HTML)
	require.NotNil(t, pages.ForgotPassword)
	require.NotNil(t, pages.Device)
	require.NotNil(t, pages.VerifyEmail)

	appearance := manifest.Auth.ManagedPages.Appearance
	require.NotNil(t, appearance)
	require.NotNil(t, appearance.Theme)
	require.NotNil(t, appearance.Theme.Colors)
	assert.Equal(t, "#1d4ed8", *appearance.Theme.Colors.Accent)
	assert.Equal(t, "humanist", *appearance.Theme.Font)
	require.NotNil(t, appearance.Layouts)
	assert.Equal(t, "split-right", *appearance.Layouts.ResetPassword)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body struct {
		Auth struct {
			ManagedPages map[string]any `json:"managed_pages"`
		} `json:"auth"`
	}
	require.NoError(t, json.Unmarshal(encoded, &body))
	assert.Equal(t, map[string]any{
		"theme": map[string]any{
			"version": float64(1),
			"colors": map[string]any{
				"background":  "#f7f5f2",
				"surface":     "#ffffff",
				"text":        "#1a1a1a",
				"accent":      "#1d4ed8",
				"accent_text": "#ffffff",
			},
			"font":    "humanist",
			"scale":   "large",
			"density": "spacious",
			"radius":  "large",
		},
		"layouts": map[string]any{
			"login":           "split-left",
			"signup":          "split-right",
			"forgot_password": "centered",
			"device":          "split-left",
			"verify_email":    "centered",
			"reset_password":  "split-right",
		},
	}, body.Auth.ManagedPages["appearance"])
	uploadedPages, ok := body.Auth.ManagedPages["pages"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, uploadedPages, 6)
}

// A typo inside appearance is still refused locally, like any other field.
func TestParseManagedPagesAppearanceRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(`version: 1
auth:
  managed_pages:
    appearance:
      theme:
        colour: '#ffffff'
`), noEnv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "colour")
}
