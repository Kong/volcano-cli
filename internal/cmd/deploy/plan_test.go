package deploy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSameOriginComparesEffectivePorts(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"http://localhost:8000", "http://localhost:8000/", true},
		{"http://localhost", "http://localhost:80", true},
		{"https://LOCALHOST", "https://localhost:443", true},
		{"http://localhost", "https://localhost", false},
		{"http://localhost:8000", "http://localhost:8001", false},
		{"http://localhost:8000", "http://127.0.0.1:8000", false},
		{"", "http://localhost", false},
	} {
		assert.Equal(t, tc.want, sameOrigin(tc.a, tc.b), "%s vs %s", tc.a, tc.b)
	}
}
