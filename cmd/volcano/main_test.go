package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// withInstructions drives api.LastInstructions() through a real api.Client
// call against a test server, mirroring how production populates it from
// response headers (VOL-180).
func withInstructions(t *testing.T, latest, deviceInstruction string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if latest != "" {
			w.Header().Set("X-Volcano-CLI-Latest-Version", latest)
		}
		if deviceInstruction != "" {
			w.Header().Set("X-Volcano-Device-Instruction", deviceInstruction)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	t.Cleanup(server.Close)

	client, err := api.NewClient(server.URL, "", api.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	_, err = client.ListProjects(context.Background(), api.DefaultPage, api.DefaultLimit)
	require.NoError(t, err)
}

func TestPrintDeprecationError_WithLatestVersion(t *testing.T) {
	withInstructions(t, "v1.5.0", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired, Message: "cli version no longer supported; run `volcano upgrade`"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 426: cli version no longer supported; run `volcano upgrade`")
	assert.Contains(t, out.String(), "Upgrade to v1.5.0: volcano upgrade")
}

func TestPrintDeprecationError_WithoutLatestVersion(t *testing.T) {
	withInstructions(t, "", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired, Message: "cli version no longer supported"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 426: cli version no longer supported")
	assert.NotContains(t, out.String(), "Upgrade to")
}

func TestPrintDeprecationError_UsesCommandPathPrefix(t *testing.T) {
	withInstructions(t, "v1.5.0", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired}, cliruntime.Deps{CommandPathPrefix: "acme"})

	assert.Contains(t, out.String(), "Upgrade to v1.5.0: acme upgrade")
}

func TestPrintError_ReauthHint(t *testing.T) {
	withInstructions(t, "", "reauth")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusUnauthorized, Message: "token expired"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 401: token expired")
	assert.Contains(t, out.String(), "Run `volcano login` to re-authenticate.")
}

func TestPrintError_NoReauthHintWithoutSignal(t *testing.T) {
	withInstructions(t, "", "")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusInternalServerError, Message: "boom"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 500: boom")
	assert.NotContains(t, out.String(), "re-authenticate")
}

func TestPrintError_ReauthHintUsesCommandPathPrefix(t *testing.T) {
	withInstructions(t, "", "reauth")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusUnauthorized}, cliruntime.Deps{CommandPathPrefix: "acme"})

	assert.Contains(t, out.String(), "Run `acme login` to re-authenticate.")
}
