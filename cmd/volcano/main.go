// Volcano is the CLI entry point.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

func main() {
	deps := cliruntime.Deps{}
	os.Exit(run(rootcmd.New(deps), deps))
}

// run executes root and returns the process exit code. Extracted from main
// so this orchestration — 426 short-circuiting, error-before-notices
// ordering, and exit codes — is covered by tests instead of only by the
// individually-tested helper functions it calls. Uses root.ErrOrStderr()
// (defaults to os.Stderr, same as before this extraction) so tests can
// redirect it via root.SetErr.
func run(root *cobra.Command, deps cliruntime.Deps) int {
	err := root.Execute()
	stderr := root.ErrOrStderr()

	if err != nil && api.Status(err) == http.StatusUpgradeRequired {
		// The 426 body's message already reads "cli version no longer
		// supported; run `volcano upgrade`"; just add the concrete upgrade
		// target when the API provided one, and stop — printing the
		// suggestion/deprecation notice below too would repeat ourselves.
		printDeprecationError(stderr, err, deps)
		return 1
	}

	if err != nil {
		// Print the failure first: stderr's first line should be the actual
		// error, for both humans skimming and log parsers/scripts that treat
		// line 1 as the failure reason. Any pending notice is secondary
		// context, printed after — VOL-180 instructions observed from an
		// earlier API call in this invocation are not cleared by whatever
		// unrelated error ended the command (see api's recordInstructions).
		printError(stderr, err, deps)
		upgradecmd.PrintAPIInstructionNotices(root, deps)
		return 1
	}

	// Success: covers the non-blocking suggestion, and a deprecated CLI
	// succeeding on an exempt route (e.g. `login`) where the user still needs
	// to know their CLI is deprecated even though this command was let
	// through. Reads only in-process state (api.LastInstructions); adds no
	// network call.
	upgradecmd.PrintAPIInstructionNotices(root, deps)
	return 0
}

// printDeprecationError renders a require_version_upgrade 426 error together with
// the concrete upgrade target, when the API provided one.
func printDeprecationError(w io.Writer, err error, deps cliruntime.Deps) {
	fmt.Fprintln(w, theme.Error("Error:", theme.On(w)), err)
	if latest := api.LastInstructions().LatestVersion; latest != "" {
		fmt.Fprintf(w, "Upgrade to %s: %s\n", latest, cliruntime.CommandPath(deps, "upgrade"))
	}
}

// printError renders a generic command error, appending a reauth hint when
// the API signaled the platform token needs re-authentication (VOL-180).
func printError(w io.Writer, err error, deps cliruntime.Deps) {
	fmt.Fprintln(w, theme.Error("Error:", theme.On(w)), err)
	if api.LastInstructions().DeviceInstruction == api.DeviceInstructionReauth {
		fmt.Fprintf(w, "Run `%s` to re-authenticate.\n", cliruntime.CommandPath(deps, "login"))
	}
	printProjectTokenMismatchHint(w, err)
}

// wrongProjectRefusal is how the platform names a project access token used
// against a project other than its own — the one 403 this hint explains.
const wrongProjectRefusal = "project access token is not valid for this project"

// printProjectTokenMismatchHint explains the likeliest cause of a 403 when the
// credential is a project access token.
//
// A pt- token is bound to one project, but the token and the project resolve
// independently: setting VOLCANO_TOKEN on a machine that has already logged in
// leaves the project as whatever `volcano use` selected last. The request then
// carries one project's credential to another project's URL, and the server can
// only answer 403. Whether they match is not knowable here without a round
// trip, so this names the possibility rather than asserting it.
func printProjectTokenMismatchHint(w io.Writer, err error) {
	if api.Status(err) != http.StatusForbidden {
		return
	}
	if !mismatchCouldExplain(api.Message(err)) {
		return
	}
	cfg, cfgErr := config.Load()
	if cfgErr != nil || !config.IsProjectToken(cfg.Token()) {
		return
	}
	projectID := cfg.ProjectID()
	if projectID == "" {
		return
	}
	fmt.Fprintf(w,
		"This ran against project %s. A project access token only works on the project it was created in — "+
			"check that is the right one, or run `volcano use <project-id>` to switch.\n",
		projectID)
}

// unexplainedRefusals are the 403 bodies that give no reason of their own: the
// platform's generic denials, and an empty body from something in front of it.
var unexplainedRefusals = []string{"", "forbidden", "access denied"}

// mismatchCouldExplain reports whether a 403 body leaves room for the mismatch
// hint.
//
// Two kinds do. The wrong-project refusal is the one this hint exists for, and
// a body that gives no reason at all leaves the mismatch the likeliest
// reading. Every other refusal already carries its own reason —
// a read-only scope, an account-scoped route, a plan-gated feature — and
// advising a project switch on top of one sends the user to change the thing
// that was already right.
//
// This is an allowlist because the alternative cannot hold: the platform grows
// new 403s, and each one that happens not to mention the credential would
// inherit the hint. The plan gate ("feature is not available on this plan") is
// exactly that case.
func mismatchCouldExplain(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(message, wrongProjectRefusal) {
		return true
	}
	return slices.Contains(unexplainedRefusals, message)
}
