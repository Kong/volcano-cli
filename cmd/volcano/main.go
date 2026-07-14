// Volcano is the CLI entry point.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
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
	// ExecuteC returns the command that actually ran (the resolved leaf), even
	// when it errored. PrintAPIInstructionNotices needs that leaf — not root —
	// to read its CreditPromptSafeAnnotation and decide whether the interactive
	// credit-gate prompt is allowed. Output still resolves through the shared
	// stderr/stdin inherited from root.
	executed, err := root.ExecuteC()
	if executed == nil {
		executed = root
	}
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
		upgradecmd.PrintAPIInstructionNotices(executed, deps)
		return 1
	}

	// Success: covers the non-blocking suggestion, and a deprecated CLI
	// succeeding on an exempt route (e.g. `login`) where the user still needs
	// to know their CLI is deprecated even though this command was let
	// through. Reads only in-process state (api.LastInstructions); adds no
	// network call.
	upgradecmd.PrintAPIInstructionNotices(executed, deps)
	return 0
}

// printDeprecationError renders a require_version_upgrade 426 error together with
// the concrete upgrade target, when the API provided one.
func printDeprecationError(w io.Writer, err error, deps cliruntime.Deps) {
	fmt.Fprintln(w, "Error:", err)
	if latest := api.LastInstructions().LatestVersion; latest != "" {
		fmt.Fprintf(w, "Upgrade to %s: %s\n", latest, cliruntime.CommandPath(deps, "upgrade"))
	}
}

// printError renders a generic command error, appending a reauth hint when
// the API signaled the platform token needs re-authentication (VOL-180).
func printError(w io.Writer, err error, deps cliruntime.Deps) {
	fmt.Fprintln(w, "Error:", err)
	if api.LastInstructions().DeviceInstruction == api.DeviceInstructionReauth {
		fmt.Fprintf(w, "Run `%s` to re-authenticate.\n", cliruntime.CommandPath(deps, "login"))
	}
}
