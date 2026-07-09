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

	// CLIInstructionLowCreditWarning and CLIInstructionNotEnoughCredit are
	// RESERVED for a future billing/credit gate. PrintAPIInstructionNotices
	// already has cases for both, but they're unreached today: the API does
	// not emit them yet — billing integration into this protocol needs its own
	// design pass. Naming is locked in now, parallel to the version
	// instructions, so a future server-side implementation doesn't need a
	// wire-format rename.
	CLIInstructionLowCreditWarning = "low_credit_warning"
	CLIInstructionNotEnoughCredit  = "not_enough_credit"

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

// recordInstructions updates the two instruction fields independently, and
// only when a response actually carries a value for that field — a response
// with no X-Volcano-CLI-Instruction header does not clear a CLIInstruction
// (and its paired LatestVersion) recorded from an earlier response in the
// same command invocation, and likewise for DeviceInstruction. A command
// that makes several API calls must not have an earlier real notice silently
// dropped just because a later, unrelated response didn't repeat the header.
//
// CLIInstruction and LatestVersion update together (never independently):
// they come from the same server-side gate decision, so a response that sets
// one is authoritative for both, even if that response's LatestVersion is
// itself empty (e.g. no latest configured).
func recordInstructions(header http.Header) {
	cliInstruction := strings.TrimSpace(header.Get(headerCLIInstruction))
	latestVersion := strings.TrimSpace(header.Get(headerCLILatestVersion))
	deviceInstruction := strings.TrimSpace(header.Get(headerDeviceInstruction))

	instructionsMu.Lock()
	defer instructionsMu.Unlock()
	if cliInstruction != "" {
		lastInstructions.CLIInstruction = cliInstruction
		lastInstructions.LatestVersion = latestVersion
	}
	if deviceInstruction != "" {
		lastInstructions.DeviceInstruction = deviceInstruction
	}
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

// ResetLastInstructionsForTest clears the process-global instructions state.
// Test-only: production never needs this, since each CLI invocation is a
// fresh process. Call it between test cases that expect LastInstructions() to
// start from the zero value — recordInstructions is intentionally sticky (see
// its doc comment): a response that omits a field never clears a previously
// recorded value for it, so tests must reset explicitly rather than relying
// on an unrelated response to clear prior state.
func ResetLastInstructionsForTest() {
	instructionsMu.Lock()
	lastInstructions = Instructions{}
	instructionsMu.Unlock()
}
