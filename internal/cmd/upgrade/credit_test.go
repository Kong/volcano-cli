package upgrade

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const testBillingURL = "https://volcano.dev/billing?source=cli"

// fixedWebDeps returns Deps whose config resolves to the compiled default web
// URL (https://volcano.dev) regardless of the host environment, so billing-URL
// assertions are deterministic.
func fixedWebDeps() cliruntime.Deps {
	return cliruntime.Deps{
		ConfigLoader: func() (*config.Config, error) {
			return &config.Config{IgnoreEnv: true}, nil
		},
	}
}

// forceTerminal overrides the package-level terminalCheck for the duration of a
// test so the interactive-prompt path can be exercised without a real TTY.
func forceTerminal(t *testing.T, isTTY bool) {
	t.Helper()
	original := terminalCheck
	terminalCheck = func(any) bool { return isTTY }
	t.Cleanup(func() { terminalCheck = original })
}

// promptSafeCmd builds a command opted into the credit prompt with wired stdin
// and a captured stderr buffer.
func promptSafeCmd(stdin string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{
		Use:         "deploy",
		Annotations: map[string]string{CreditPromptSafeAnnotation: "true"},
	}
	var out bytes.Buffer
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	return cmd, &out
}

func TestHandleCreditNotice_PromptOpensBrowserOnConsent(t *testing.T) {
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")
	forceTerminal(t, true)
	t.Setenv("CI", "")

	cmd, out := promptSafeCmd("y\n")
	var opened string
	deps := fixedWebDeps()
	deps.OpenBrowser = func(url string) error { opened = url; return nil }

	PrintAPIInstructionNotices(cmd, deps)

	assert.Equal(t, testBillingURL, opened)
	assert.Contains(t, out.String(), "Open the billing page to purchase credits?")
	// On a successful browser open the URL fallback line is not printed.
	assert.NotContains(t, out.String(), "Purchase credits at:")
}

func TestHandleCreditNotice_DeclinePreservesAndPrintsURL(t *testing.T) {
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")
	forceTerminal(t, true)
	t.Setenv("CI", "")

	cmd, out := promptSafeCmd("n\n")
	browserCalled := false
	deps := fixedWebDeps()
	deps.OpenBrowser = func(string) error { browserCalled = true; return nil }

	PrintAPIInstructionNotices(cmd, deps)

	assert.False(t, browserCalled, "declining must not open the browser")
	assert.Contains(t, out.String(), "Purchase credits at: "+testBillingURL)
}

func TestHandleCreditNotice_BrowserOpenFailurePrintsURL(t *testing.T) {
	withInstructions(t, api.CLIInstructionLowCreditWarning, "", "")
	forceTerminal(t, true)
	t.Setenv("CI", "")

	cmd, out := promptSafeCmd("yes\n")
	deps := fixedWebDeps()
	deps.OpenBrowser = func(string) error { return assert.AnError }

	PrintAPIInstructionNotices(cmd, deps)

	assert.Contains(t, out.String(), "Could not open your browser. Purchase credits at: "+testBillingURL)
}

func TestHandleCreditNotice_PromptSuppressedWithoutAnnotation(t *testing.T) {
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")
	forceTerminal(t, true)
	t.Setenv("CI", "")

	cmd := &cobra.Command{Use: "deploy"} // no CreditPromptSafeAnnotation
	var out bytes.Buffer
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("y\n"))
	browserCalled := false
	deps := fixedWebDeps()
	deps.OpenBrowser = func(string) error { browserCalled = true; return nil }

	PrintAPIInstructionNotices(cmd, deps)

	assert.False(t, browserCalled)
	assert.NotContains(t, out.String(), "Open the billing page")
	assert.Contains(t, out.String(), "Purchase credits at: "+testBillingURL)
}

func TestHandleCreditNotice_PromptSuppressedInCI(t *testing.T) {
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")
	forceTerminal(t, true)
	t.Setenv("CI", "true")

	cmd, out := promptSafeCmd("y\n")
	browserCalled := false
	deps := fixedWebDeps()
	deps.OpenBrowser = func(string) error { browserCalled = true; return nil }

	PrintAPIInstructionNotices(cmd, deps)

	assert.False(t, browserCalled, "must never prompt or open a browser under CI")
	assert.NotContains(t, out.String(), "Open the billing page")
	assert.Contains(t, out.String(), "Purchase credits at: "+testBillingURL)
}

func TestHandleCreditNotice_PromptSuppressedNonTTY(t *testing.T) {
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")
	forceTerminal(t, false) // not a terminal
	t.Setenv("CI", "")

	cmd, out := promptSafeCmd("y\n")
	browserCalled := false
	deps := fixedWebDeps()
	deps.OpenBrowser = func(string) error { browserCalled = true; return nil }

	PrintAPIInstructionNotices(cmd, deps)

	assert.False(t, browserCalled)
	assert.NotContains(t, out.String(), "Open the billing page")
	assert.Contains(t, out.String(), "Purchase credits at: "+testBillingURL)
}

func TestBillingURL_RespectsWebURLOverride(t *testing.T) {
	t.Setenv("VOLCANO_WEB_URL", "https://staging.volcano.example")
	// IgnoreEnv=false so the VOLCANO_WEB_URL override is honored.
	deps := cliruntime.Deps{ConfigLoader: func() (*config.Config, error) {
		return &config.Config{}, nil
	}}

	assert.Equal(t, "https://staging.volcano.example/billing?source=cli", billingURL(deps))
}

func TestBillingURL_FallsBackToWebOriginOnInvalidURL(t *testing.T) {
	deps := cliruntime.Deps{ConfigLoader: func() (*config.Config, error) {
		// A non-http(s) scheme fails WebBillingURL validation; billingURL must
		// still return a usable value (the origin) rather than an empty string.
		return &config.Config{}, nil
	}}
	t.Setenv("VOLCANO_WEB_URL", "ftp://example.com")

	assert.Equal(t, "ftp://example.com", billingURL(deps))
}

func TestWebBillingURL(t *testing.T) {
	got, err := api.WebBillingURL("https://volcano.dev/")
	require.NoError(t, err)
	assert.Equal(t, testBillingURL, got)

	_, err = api.WebBillingURL("")
	require.Error(t, err)

	_, err = api.WebBillingURL("ftp://example.com")
	require.Error(t, err)
}
