// Package root assembles the top-level volcano command and its subcommands.
package root

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	authcmd "github.com/Kong/volcano-cli/internal/cmd/auth"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	databasescmd "github.com/Kong/volcano-cli/internal/cmd/databases"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	initcmd "github.com/Kong/volcano-cli/internal/cmd/init"
	localcmd "github.com/Kong/volcano-cli/internal/cmd/local"
	localmodecmd "github.com/Kong/volcano-cli/internal/cmd/localmode"
	projectcmd "github.com/Kong/volcano-cli/internal/cmd/project"
	storagecmd "github.com/Kong/volcano-cli/internal/cmd/storage"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
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
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			upgradecmd.MaybePrintUpdateNotice(cmd, deps)
		},
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
	root.AddCommand(authcmd.NewLogout())
	root.AddCommand(initcmd.New())
	root.AddCommand(projectcmd.NewProjects(deps))
	root.AddCommand(projectcmd.NewUse(deps))
	root.AddCommand(configcmd.New(deps))
	root.AddCommand(databasescmd.New(deps))
	root.AddCommand(frontendscmd.New(deps))
	root.AddCommand(functionscmd.New(deps))
	root.AddCommand(localmodecmd.NewStart(deps))
	root.AddCommand(localmodecmd.NewStatus(deps))
	root.AddCommand(localmodecmd.NewStop(deps))
	root.AddCommand(localmodecmd.NewRestart(deps))
	root.AddCommand(localcmd.New(deps))
	root.AddCommand(storagecmd.New(deps))
	root.AddCommand(variablescmd.New(deps))
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
