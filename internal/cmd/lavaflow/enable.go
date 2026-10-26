package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type enableOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	opts := enableOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Turn LavaFlow on",
		Long: clilavaflow.OnDescription + `

This turns LavaFlow on for the current project. By default, runs start
without a request; a project configuration with
auto_trigger: false keeps them to '` + cliruntime.CommandPath(deps, "lavaflow runs start") + `'.
Each run counts toward the project's usage.
` + clilavaflow.CostNote + `

Turning LavaFlow on needs a paid plan and a person's login: a project access
token is refused. Connect a repository with 'volcano git connect' so LavaFlow
can open fix pull requests; without one it diagnoses issues only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runEnable(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runEnable(ctx context.Context, opts enableOptions) error {
	service := clilavaflow.NewService(opts.deps)
	settings, err := service.SetEnabled(ctx, true)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	// LavaFlow is on by now, so a cancel during the repository read leaves
	// the line about it general rather than fail a change that was made.
	state, repository, _ := service.Repository(ctx)
	output.LavaFlowEnabled(opts.out, output.LavaFlowEnabledDetails{
		Settings:          settings,
		Repository:        repository,
		RepositoryUnknown: state != clilavaflow.RepositoryConnected && state != clilavaflow.RepositoryNotConnected,
		Description:       clilavaflow.OnDescription,
		CostNote:          clilavaflow.CostNote,
		GitConnectCommand: gitConnectCommand,
	})
	return nil
}
