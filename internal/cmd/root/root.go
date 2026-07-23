// Package root assembles the top-level volcano command and its subcommands.
package root

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	authcmd "github.com/Kong/volcano-cli/internal/cmd/auth"
	cloudcmd "github.com/Kong/volcano-cli/internal/cmd/cloud"
	docscmd "github.com/Kong/volcano-cli/internal/cmd/docs"
	initcmd "github.com/Kong/volcano-cli/internal/cmd/init"
	localcmd "github.com/Kong/volcano-cli/internal/cmd/local"
	localmodecmd "github.com/Kong/volcano-cli/internal/cmd/localmode"
	projectcmd "github.com/Kong/volcano-cli/internal/cmd/project"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/version"
)

// New returns the root Volcano command.
func New(deps cliruntime.Deps) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "volcano",
		Short:         "Volcano CLI",
		Long:          "volcano is the command-line client for the Volcano hosting platform.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				printVersion(cmd.OutOrStdout())
				return nil
			}
			return cmd.Help()
		},
	}
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print CLI version")
	root.AddCommand(newVersionCmd())
	root.AddCommand(upgradecmd.New(deps))
	root.AddCommand(authcmd.NewLogin(deps))
	root.AddCommand(authcmd.NewSignup(deps))
	root.AddCommand(authcmd.NewLogout())
	root.AddCommand(initcmd.New())
	root.AddCommand(docscmd.New(deps))
	root.AddCommand(projectcmd.NewProjects(deps))
	root.AddCommand(projectcmd.NewUse(deps))
	root.AddCommand(localmodecmd.NewStart(deps))
	root.AddCommand(localmodecmd.NewStatus(deps))
	root.AddCommand(localmodecmd.NewStop(deps))
	root.AddCommand(localmodecmd.NewRestart(deps))
	root.AddCommand(localcmd.NewResourceCommands(deps)...)
	root.AddCommand(cloudcmd.New(deps))
	root.AddCommand(cloudcmd.NewDeprecatedFrontendAlias(deps))
	root.AddCommand(localcmd.New(deps))
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			printVersion(cmd.OutOrStdout())
			return nil
		},
	}
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "volcano %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
}
