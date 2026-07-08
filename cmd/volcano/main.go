// Volcano is the CLI entry point.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func main() {
	deps := cliruntime.Deps{}
	root := rootcmd.New(deps)
	err := root.Execute()

	if err != nil && api.Status(err) == http.StatusUpgradeRequired {
		// The 426 body's message already reads "cli version no longer
		// supported; run `volcano upgrade`"; just add the concrete upgrade
		// target when the API provided one, and stop — printing the
		// suggestion/deprecation notice below too would repeat ourselves.
		printDeprecationError(os.Stderr, err, deps)
		os.Exit(1)
	}

	// Print any VOL-180 upgrade/deprecation notice unconditionally otherwise:
	// covers the non-blocking suggestion, and a deprecated CLI succeeding on
	// an exempt route (e.g. `login`) where the user still needs to know their
	// CLI is deprecated even though this particular command was let through.
	// Reads only in-process state (api.LastInstructions); adds no network call.
	upgradecmd.PrintAPIInstructionNotices(root, deps)

	if err != nil {
		printError(os.Stderr, err, deps)
		os.Exit(1)
	}
}

// printDeprecationError renders a version_deprecation 426 error together with
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
