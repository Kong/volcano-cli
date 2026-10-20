package deploylink

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLinkTakesTheID(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"bare id", "k3Vn8TqPz0aX"},
		{"bare id with spaces", "  k3Vn8TqPz0aX\n"},
		{"url", "https://volcano.dev/deploy/k3Vn8TqPz0aX"},
		{"url with a trailing slash", "https://volcano.dev/deploy/k3Vn8TqPz0aX/"},
		{"url with a query and a fragment", "https://volcano.dev/deploy/k3Vn8TqPz0aX?ref=readme#top"},
		{"any host", "https://staging.example.test:8443/deploy/k3Vn8TqPz0aX"},
		{"http", "http://localhost:3000/deploy/k3Vn8TqPz0aX"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseLink(tc.value)
			require.NoError(t, err)
			assert.Equal(t, "k3Vn8TqPz0aX", id)
		})
	}
}

func TestParseLinkRefusesAnythingElse(t *testing.T) {
	for _, value := range []string{
		"",
		"k3Vn8TqPz0a",   // 11 characters
		"k3Vn8TqPz0aXY", // 13 characters
		"k3Vn8TqPz0a-",  // not alphanumeric
		"volcano.dev/deploy/k3Vn8TqPz0aX",
		"ftp://volcano.dev/deploy/k3Vn8TqPz0aX",
		"https:///deploy/k3Vn8TqPz0aX",
		"https://volcano.dev/k3Vn8TqPz0aX",
		"https://volcano.dev/deploy/",
		"https://volcano.dev/deploy/short",
		"https://volcano.dev/deploy/k3Vn8TqPz0aX/extra",
		"https://volcano.dev/deploy/k3Vn8TqPz0aX/../deploy/other",
		"https://volcano.dev/deploys/k3Vn8TqPz0aX",
		"https://volcano.dev/x/deploy/k3Vn8TqPz0aX",
		"https://volcano.dev/deploy/k3Vn8TqPz0%2FX",
	} {
		t.Run(value, func(t *testing.T) {
			_, err := ParseLink(value)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid deploy link")
		})
	}
}
