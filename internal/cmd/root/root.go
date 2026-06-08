// Package root assembles the top-level volcano command and its subcommands.
package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

const updateCheckTimeout = 2 * time.Second

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
			maybePrintUpdateNotice(cmd, deps)
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
	root.AddCommand(newUpgradeCmd(deps))
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

func newUpgradeCmd(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Volcano CLI to the latest release",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return update.Upgrade(cmd.Context(), version.Version, cmd.OutOrStdout(), updateOptions(deps))
		},
	}
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

func maybePrintUpdateNotice(cmd *cobra.Command, deps cliruntime.Deps) {
	if shouldSkipUpdateCheck(cmd) {
		return
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), updateCheckTimeout)
	defer cancel()
	notice, err := update.CheckLatest(ctx, version.Version, updateOptions(deps))
	if err != nil {
		if errors.Is(err, update.ErrNoUpdateAvailable) {
			return
		}
		return
	}
	if notice == nil {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "A newer Volcano CLI version is available: %s (current %s). Run `volcano upgrade` to upgrade.\n", notice.Latest, notice.Current)
}

func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if version.Version == "dev" {
		return true
	}
	if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") {
		return true
	}
	if cmd.Parent() == nil && len(cmd.Flags().Args()) == 0 {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "version" || c.Name() == "upgrade" || c.Name() == "help" {
			return true
		}
	}
	for _, arg := range os.Args[1:] {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "--help" || trimmed == "-h" || trimmed == "help" || trimmed == "--version" || trimmed == "-v" {
			return true
		}
	}
	return false
}

func updateOptions(deps cliruntime.Deps) update.Options {
	return update.Options{
		HTTPClient:     deps.HTTPClient,
		GitHubAPIURL:   deps.UpdateGitHubAPIURL,
		ExecutablePath: deps.ExecutablePath,
		CommandRunner:  deps.UpdateCommandRunner,
	}
}
