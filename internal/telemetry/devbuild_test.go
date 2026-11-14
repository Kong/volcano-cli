package telemetry

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/version"
)

func setVersion(t *testing.T, v string) {
	t.Helper()
	old := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = old })
}

func TestDevBuildToProduction(t *testing.T) {
	setVersion(t, "dev")
	for _, apiURL := range []string{"https://api.volcano.dev", "https://API.volcano.dev/", "https://api.volcano.dev.:443/v1"} {
		assert.True(t, DevBuildToProduction(apiURL), apiURL)
	}
	for _, apiURL := range []string{
		"https://api.staging.volcano.dev",
		"http://localhost:8080",
		"http://127.0.0.1:8080",
		"https://api.volcano.dev.example.com",
		"https://volcano.dev",
		"://bad",
	} {
		assert.False(t, DevBuildToProduction(apiURL), apiURL)
	}

	setVersion(t, "v0.60.0")
	assert.False(t, DevBuildToProduction("https://api.volcano.dev"), "a release build sends to production")
}

// recordingTransport answers every request without the network and records it.
type recordingTransport struct{ urls []string }

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.urls = append(rt.urls, req.URL.String())
	return &http.Response{StatusCode: http.StatusAccepted, Body: http.NoBody, Request: req}, nil
}

func TestFlush_DevBuildSendsNothingToProduction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_API_URL", "https://api.volcano.dev")
	transport := &recordingTransport{}
	client := &http.Client{Transport: transport}

	setVersion(t, "dev")
	require.NoError(t, Flush(t.Context(), strings.NewReader(`{"device_id":"d"}`), client))
	assert.Empty(t, transport.urls, "a development build drops the event")

	setVersion(t, "v0.60.0")
	require.NoError(t, Flush(t.Context(), strings.NewReader(`{"device_id":"d"}`), client))
	assert.Equal(t, []string{"https://api.volcano.dev/cli/telemetry"}, transport.urls, "a release build sends it")
}
