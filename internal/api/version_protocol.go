package api

import (
	"net/http"
	"runtime"
	"strings"
	"sync"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/version"
)

// Header names and instruction values for the VOL-180 CLI version protocol.
// These MUST match volcano-hosting's internal/constants/http.go and
// docs/cli/version-gating.md. Duplicated here because this is a separate
// repository with no shared Go module; there is intentionally no
// cryptographic binding on the request header — see "Security model" in that
// doc for why the claimed version is advisory input only and never a
// privilege (the CLI cannot make this header trustworthy, and doesn't need
// to: the API never grants anything based on it).
const (
	headerCLIVersion        = "X-Volcano-CLI-Version"
	headerCLIInstruction    = "X-Volcano-CLI-Instruction"
	headerCLILatestVersion  = "X-Volcano-CLI-Latest-Version"
	headerDeviceInstruction = "X-Volcano-Device-Instruction"

	// CLIInstructionSuggestionVersionUpgrade signals a newer CLI is published; non-blocking.
	CLIInstructionSuggestionVersionUpgrade = "suggestion_version_upgrade"
	// CLIInstructionRequireVersionUpgrade signals this CLI version is no longer
	// supported. The API pairs it with an HTTP 426 on non-exempt routes.
	CLIInstructionRequireVersionUpgrade = "require_version_upgrade"
	// DeviceInstructionReauth signals the platform token needs re-authentication.
	DeviceInstructionReauth = "reauth"
)

// Instructions is a snapshot of the most recent VOL-180 protocol signals
// observed on any API response in this process.
type Instructions struct {
	CLIInstruction    string
	LatestVersion     string
	DeviceInstruction string
}

var (
	instructionsMu   sync.RWMutex
	lastInstructions Instructions
)

// LastInstructions returns the most recently observed VOL-180 instructions
// from any API response in this process. It is the zero value if no API call
// has completed yet — e.g. local-only commands (init, help, version,
// completion) never populate this, since they never make a request.
//
// A CLI process runs exactly one command per invocation, so "most recent" is
// simply "from the request(s) this command made" — there is no cross-command
// state to reconcile.
func LastInstructions() Instructions {
	instructionsMu.RLock()
	defer instructionsMu.RUnlock()
	return lastInstructions
}

func recordInstructions(header http.Header) {
	next := Instructions{
		CLIInstruction:    strings.TrimSpace(header.Get(headerCLIInstruction)),
		LatestVersion:     strings.TrimSpace(header.Get(headerCLILatestVersion)),
		DeviceInstruction: strings.TrimSpace(header.Get(headerDeviceInstruction)),
	}
	instructionsMu.Lock()
	lastInstructions = next
	instructionsMu.Unlock()
}

// userAgent identifies this CLI build to the API — for support/debugging and
// as the VOL-180 pre-protocol/attribution signal — replacing the Go HTTP
// client's default user agent.
func userAgent() string {
	return "volcano-cli/" + version.Version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}

// versionProtocolDoer wraps an apiclient.HttpRequestDoer to speak the VOL-180
// CLI version protocol on every request: it reports this CLI's version and
// identity, and records whatever instructions the API returns so callers can
// act on them via LastInstructions. It never alters the request/response
// beyond adding headers — bodies and errors pass through untouched, and it
// never retries.
type versionProtocolDoer struct {
	next apiclient.HttpRequestDoer
}

func (d versionProtocolDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set(headerCLIVersion, version.Version)
	req.Header.Set("User-Agent", userAgent())
	resp, err := d.next.Do(req)
	if resp != nil {
		recordInstructions(resp.Header)
	}
	return resp, err
}
