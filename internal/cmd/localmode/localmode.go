// Package localmode wires top-level local-mode environment commands.
package localmode

import (
	"github.com/spf13/cobra"

	localmodecore "github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewStart returns the start command.
func NewStart(deps cliruntime.Deps) *cobra.Command {
	var image string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the local Volcano development environment",
		Long: `Start PostgreSQL, Redis, and the Volcano local-mode server with Docker Compose.

To run a specific or locally-built server image, use --image (highest precedence)
or set VOLCANO_IMAGE:
  volcano start --image kong/volcano:local-dev
  VOLCANO_IMAGE=kong/volcano:local-nightly volcano start

An explicitly selected image must already exist locally: the CLI never pulls an
unpublished local-mode image and fails fast if it is missing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps, localmodecore.WithImage(image)).Start(cmd.Context(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "Local-mode server image to run (overrides VOLCANO_IMAGE and the bundled default; must already exist locally)")
	return cmd
}

// NewDoctor returns the doctor command.
func NewDoctor(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local development prerequisites (Docker engine + Compose)",
		Long: `Diagnose whether this machine can run 'volcano start'.

Verifies a Docker-compatible engine and Docker Compose v2 are installed and
reachable, and prints actionable fixes. Any Docker-compatible engine works
(Docker Desktop, OrbStack, Colima, Docker Engine, Podman); the CLI never
installs one for you. Exits non-zero if a prerequisite is missing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps).Doctor(cmd.Context(), cmd.OutOrStdout())
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
	var image string
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the local Volcano development environment",
		Long: `Stop and start the local Volcano development environment while preserving data.

Use --image (or VOLCANO_IMAGE) to select the server image; an explicitly
selected image must already exist locally and is never pulled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return localmodecore.NewService(deps, localmodecore.WithImage(image)).Restart(cmd.Context(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&image, "image", "", "Local-mode server image to run (overrides VOLCANO_IMAGE and the bundled default; must already exist locally)")
	return cmd
}
