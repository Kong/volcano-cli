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
	instructionsMu.Lock()
	lastInstructions = Instructions{}
	instructionsMu.Unlock()
	t.Cleanup(func() {
		instructionsMu.Lock()
		lastInstructions = Instructions{}
		instructionsMu.Unlock()
	})
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

func TestVersionProtocolDoer_RecordsInstructionHeaders(t *testing.T) {
	resetInstructions(t)

	inner := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionUpgrade)
		rec.Header().Set(headerCLILatestVersion, "v1.5.0")
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/projects", http.NoBody)
	_, err := versionProtocolDoer{next: inner}.Do(req)
	require.NoError(t, err)

	got := LastInstructions()
	assert.Equal(t, CLIInstructionSuggestionUpgrade, got.CLIInstruction)
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

func TestVersionProtocolDoer_LatestInvocationWins(t *testing.T) {
	// A CLI process runs one command per invocation; "most recent" reflects the
	// last response observed within that single run.
	resetInstructions(t)

	first := doerFunc(func(*http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set(headerCLIInstruction, CLIInstructionSuggestionUpgrade)
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
	require.Equal(t, CLIInstructionSuggestionUpgrade, LastInstructions().CLIInstruction)

	_, err = versionProtocolDoer{next: second}.Do(req)
	require.NoError(t, err)
	assert.Empty(t, LastInstructions().CLIInstruction, "a later response with no instruction header clears the prior one")
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }
