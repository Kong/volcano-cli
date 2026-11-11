// Package fork wires volcano fork: creating your own copy of a shared project.
package fork

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/fork"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the fork command.
func New(deps cliruntime.Deps) *cobra.Command {
	opts := fork.Options{Select: true}
	cmd := &cobra.Command{
		Use:   "fork <link> [name]",
		Short: "Create your own cloud copy of a shared project",
		Long: `Create your own copy of a project someone shared with 'volcano share': a new
Volcano cloud project in your account, installed from the release their share
link offers. Pass the link, such as https://volcano.dev/deploy/<id>, or its
12-character id. The copy is named after the name the link suggests unless you
give one; a name you already use gets a short suffix.

The command shows what the release creates, the variables it declares, what it
leaves out and the functions that get your copy's service key, which bypasses
row-level security, and asks you to confirm it. It installs exactly that
release, waits for it to finish, and makes the copy your active project.

No variable values are needed: the copy gets every variable the release
declares, empty unless --var NAME=value gives one, and the command lists the
ones that start empty and how to set them. If the owner shares a new release
after the preview, nothing is created: at a terminal the command shows what
changed and asks again, and with --yes it shows the new release and stops, so a
release you have not seen is never deployed. --no-wait returns as soon as the
copy is created.`,
		Example: fmt.Sprintf("  %s\n  %s\n  %s",
			cliruntime.CommandPath(cliruntime.Deps{}, "fork https://volcano.dev/deploy/k3Vn8TqPz0aX"),
			cliruntime.CommandPath(cliruntime.Deps{}, "fork k3Vn8TqPz0aX my-copy"),
			cliruntime.CommandPath(cliruntime.Deps{}, `fork k3Vn8TqPz0aX --var OPENAI_API_KEY="$OPENAI_API_KEY" --yes`)),
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			run := opts
			run.Deps = deps
			run.Link = args[0]
			if len(args) > 1 {
				run.Name = strings.TrimSpace(args[1])
			}
			run.In = cmd.InOrStdin()
			run.Stdout = cmd.OutOrStdout()
			run.Out = cmd.OutOrStdout()
			if run.JSON {
				run.Out = cmd.ErrOrStderr()
			}
			return fork.Run(cmd.Context(), run)
		},
	}
	cmd.Flags().StringArrayVar(&opts.Vars, "var", nil, "Fill in a variable as NAME=value instead of leaving it empty (repeatable)")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the confirmation")
	cmd.Flags().BoolVar(&opts.NoWait, "no-wait", false, "Return once the copy is created, without waiting for its installation")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Print the created project as JSON")
	return cmd
}
