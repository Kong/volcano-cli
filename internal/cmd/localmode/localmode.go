// Package localmode wires top-level local-mode environment commands.
package localmode

import (
	"github.com/spf13/cobra"

	localmodecore "github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewStart returns the start command.
func NewStart(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the local Volcano development environment",
		Long: `Start PostgreSQL, Redis, and the Volcano local-mode server with Docker Compose.

To override the server image, set VOLCANO_IMAGE:
  VOLCANO_IMAGE=kong/volcano:nightly volcano start`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps).Start(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// NewStatus returns the status command.
func NewStatus(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the local Volcano environment",
		Long:  "Display the current state of local Volcano services and credentials.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps).Status(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// NewStop returns the stop command.
func NewStop(deps cliruntime.Deps) *cobra.Command {
	var clean bool
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the local Volcano development environment",
		Long: `Stop all Volcano Docker services.

By default, this stops containers but keeps data volumes intact.
Use --clean to also remove all data volumes and local dev state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps).Stop(cmd.Context(), cmd.OutOrStdout(), clean)
		},
	}
	cmd.Flags().BoolVar(&clean, "clean", false, "Also remove all data volumes and local dev state")
	return cmd
}

// NewRestart returns the restart command.
func NewRestart(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the local Volcano development environment",
		Long:  "Stop and start the local Volcano development environment while preserving data.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps).Restart(cmd.Context(), cmd.OutOrStdout())
		},
	}
}
