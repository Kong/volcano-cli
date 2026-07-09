package api

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/version"
)

// resetInstructions clears process-global observed instructions between test
// cases (production never needs this: each CLI invocation is a fresh process).
func resetInstructions(t *testing.T) {
	t.Helper()
	ResetLastInstructionsForTest()
	t.Cleanup(ResetLastInstructionsForTest)
}

func TestVersionProtocolDoer_SetsVersionHeaderAndUserAgent(t *testing.T) {
	resetInstructions(t)
	oldVersion := version.Version
	version.Version = "v1.4.2"
	t.Cleanup(func() { version.Version = oldVersion })

	var gotVersionHeader, gotUA string
	inner := doerFunc(func(req *http.Request) (*http.Response, error) {
		gotVersionHeader = req.Header.Get(headerCLIVersion)
		gotUA = req.Header.Get("User-Agent")
		return httptest.NewRecorder().Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: inner}.Do(req)
	require.NoError(t, err)

	assert.Equal(t, "v1.4.2", gotVersionHeader)
	assert.Equal(t, "volcano-cli/v1.4.2 ("+runtime.GOOS+"/"+runtime.GOARCH+")", gotUA)
}

func TestVersionProtocolDoer_ReportsGitDescribeBuildsAsDev(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    string
	}{
		{name: "tagged release", version: "v1.4.2", want: "v1.4.2"},
		{name: "commit after tag", version: "v1.4.2-6-gabcdef0", want: "dev"},
		{name: "dirty tag", version: "v1.4.2-dirty", want: "dev"},
		{name: "detached source revision", version: "abcdef0", want: "dev"},
		{name: "published prerelease", version: "v1.5.0-rc.1", want: "v1.5.0-rc.1"},
		{name: "nightly release", version: "v0.0.42-nightly.20260710.1", want: "v0.0.42-nightly.20260710.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetInstructions(t)
			oldVersion := version.Version
			version.Version = tc.version
			t.Cleanup(func() { version.Version = oldVersion })

			inner := doerFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, tc.want, req.Header.Get(headerCLIVersion))
				return httptest.NewRecorder().Result(), nil
			})
			_, err := versionProtocolDoer{next: inner}.Do(httptest.NewRequest(http.MethodGet, "/projects", http.NoBody))
			require.NoError(t, err)
		})
	}
}

func TestVersionProtocolDoer_RecordsInstructionHeaders(t *testing.T) {
	resetInstructions(t)

	inner := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionVersionUpgrade)
		rec.Header().Set(headerCLILatestVersion, "v1.5.0")
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: inner}.Do(req)
	require.NoError(t, err)

	got := LastInstructions()
	assert.Equal(t, CLIInstructionSuggestionVersionUpgrade, got.CLIInstruction)
	assert.Equal(t, "v1.5.0", got.LatestVersion)
	assert.Empty(t, got.DeviceInstruction)
}

func TestVersionProtocolDoer_RecordsReauthInstruction(t *testing.T) {
	resetInstructions(t)

	inner := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerDeviceInstruction, DeviceInstructionReauth)
		rec.WriteHeader(http.StatusUnauthorized)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: inner}.Do(req)
	require.NoError(t, err)

	assert.Equal(t, DeviceInstructionReauth, LastInstructions().DeviceInstruction)
}

func TestVersionProtocolDoer_NoResponseIsNoOp(t *testing.T) {
	resetInstructions(t)

	boom := assert.AnError
	inner := doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, boom
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: inner}.Do(req)
	require.ErrorIs(t, err, boom)
	assert.Equal(t, Instructions{}, LastInstructions())
}

func TestConsumeCLIInstructionsClearsOnlyCLIFields(t *testing.T) {
	resetInstructions(t)

	header := http.Header{}
	header.Set(headerCLIInstruction, CLIInstructionSuggestionVersionUpgrade)
	header.Set(headerCLILatestVersion, "v1.5.0")
	header.Set(headerDeviceInstruction, DeviceInstructionReauth)
	recordInstructions(header)

	got := ConsumeCLIInstructions()
	assert.Equal(t, CLIInstructionSuggestionVersionUpgrade, got.CLIInstruction)
	assert.Equal(t, "v1.5.0", got.LatestVersion)
	assert.Equal(t, DeviceInstructionReauth, got.DeviceInstruction)
	assert.Equal(t, Instructions{DeviceInstruction: DeviceInstructionReauth}, LastInstructions())
}

func TestVersionProtocolDoer_LaterEmptyResponseDoesNotClearEarlierInstruction(t *testing.T) {
	// A command can make several API calls; a later response that simply
	// doesn't repeat the instruction header (e.g. an unrelated call) must not
	// silently drop a real notice observed earlier in the same invocation.
	resetInstructions(t)

	first := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionVersionUpgrade)
		rec.Header().Set(headerCLILatestVersion, "v1.5.0")
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})
	second := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: first}.Do(req)
	require.NoError(t, err)
	require.Equal(t, CLIInstructionSuggestionVersionUpgrade, LastInstructions().CLIInstruction)
	require.Equal(t, "v1.5.0", LastInstructions().LatestVersion)

	_, err = versionProtocolDoer{next: second}.Do(req)
	require.NoError(t, err)
	assert.Equal(t, CLIInstructionSuggestionVersionUpgrade, LastInstructions().CLIInstruction, "a later response with no instruction header must not clear an earlier real one")
	assert.Equal(t, "v1.5.0", LastInstructions().LatestVersion, "LatestVersion stays paired with the preserved CLIInstruction")
}

func TestVersionProtocolDoer_LaterNonEmptyResponseUpdatesInstruction(t *testing.T) {
	// A genuinely different instruction (e.g. the policy changed, or a later
	// call reports a different version comparison) must still take effect —
	// preservation only protects against being cleared by an empty response.
	resetInstructions(t)

	first := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionVersionUpgrade)
		rec.Header().Set(headerCLILatestVersion, "v1.5.0")
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})
	second := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionRequireVersionUpgrade)
		rec.Header().Set(headerCLILatestVersion, "v1.6.0")
		rec.WriteHeader(http.StatusUpgradeRequired)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: first}.Do(req)
	require.NoError(t, err)
	_, err = versionProtocolDoer{next: second}.Do(req)
	require.NoError(t, err)

	assert.Equal(t, CLIInstructionRequireVersionUpgrade, LastInstructions().CLIInstruction)
	assert.Equal(t, "v1.6.0", LastInstructions().LatestVersion)
}

func TestVersionProtocolDoer_CLIInstructionAndDeviceInstructionTrackIndependently(t *testing.T) {
	// A response that sets DeviceInstruction but not CLIInstruction (or vice
	// versa) must not clear whichever one it didn't mention.
	resetInstructions(t)

	first := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionVersionUpgrade)
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})
	second := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerDeviceInstruction, DeviceInstructionReauth)
		rec.WriteHeader(http.StatusUnauthorized)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: first}.Do(req)
	require.NoError(t, err)
	_, err = versionProtocolDoer{next: second}.Do(req)
	require.NoError(t, err)

	got := LastInstructions()
	assert.Equal(t, CLIInstructionSuggestionVersionUpgrade, got.CLIInstruction, "unrelated device-instruction response must not clear the earlier CLI instruction")
	assert.Equal(t, DeviceInstructionReauth, got.DeviceInstruction)
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }
