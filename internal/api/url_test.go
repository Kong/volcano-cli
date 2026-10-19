package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		assert.Equal(t, want, PublicURL(raw), raw)
	}
}

func TestRedactURL(t *testing.T) {
	assert.Equal(t, "https://user:xxxxx@api.example.com", RedactURL("https://user:secret@api.example.com"))
	assert.Equal(t, "https://user@api.example.com", RedactURL("https://user@api.example.com"))
	assert.Equal(t, "https://api.example.com/?a=b", RedactURL("https://api.example.com/?a=b"))
}
