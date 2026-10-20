package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://api.volcano.dev":                       "https://api.volcano.dev",
		"http://localhost:8000/":                        "http://localhost:8000/",
		"https://user:secret@api.example.com":           "https://api.example.com",
		"https://user@api.example.com/v1":               "https://api.example.com/v1",
		"https://api.example.com/v1?token=abc#fragment": "https://api.example.com/v1",
		"https://api.example.com?":                      "https://api.example.com",
	} {
		got, err := PublicURL(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

// A URL that doesn't parse can't be cleaned, so it is refused, and the error
// doesn't repeat the password or the query.
func TestPublicURLRefusesAMalformedURL(t *testing.T) {
	got, err := PublicURL("https://user:secret@example.com/%zz?token=abc")
	require.Error(t, err)
	assert.Empty(t, got)
	assert.NotContains(t, err.Error(), "secret")
	assert.NotContains(t, err.Error(), "token=abc")
}

func TestRedactURL(t *testing.T) {
	assert.Equal(t, "https://user:xxxxx@api.example.com", RedactURL("https://user:secret@api.example.com"))
	assert.Equal(t, "https://user@api.example.com", RedactURL("https://user@api.example.com"))
	assert.Equal(t, "https://api.example.com/?a=b", RedactURL("https://api.example.com/?a=b"))
}
