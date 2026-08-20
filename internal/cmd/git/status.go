package git

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type statusOptions struct {
	deps cliruntime.Deps
	out  io.Writer
}

func newStatus(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current project's Git connection",
		Long: `Show which repository the current project is connected to, and what a push
to it deploys.

This reads the project's own binding and nothing else: it does not contact
GitHub, so it reports what the platform has recorded rather than whether that
recording still works. A project with nothing connected is not an error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatus(cmd.Context(), statusOptions{deps: deps, out: cmd.OutOrStdout()})
		},
	}
	return cmd
}

func runStatus(ctx context.Context, opts statusOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	project, err := service.Project()
	if err != nil {
		return err
	}

	connection, err := service.Status(ctx)
	if err != nil {
		// Having no repository connected is a state to report, not a failure:
		// answering "what is connected?" with "nothing" is a complete answer,
		// and exiting non-zero for it would make the command useless in a
		// conditional.
		if errors.Is(err, gitconnect.ErrNotConnected) {
			output.GitNotConnected(opts.out, project.Label(), cliruntime.CommandPath(opts.deps, "git connect"))
			return nil
		}
		return guide(opts.deps, webURL, err)
	}

	settings, settingsErr := service.DeploySettings(ctx)
	output.GitStatus(opts.out, output.GitBinding{
		Connection:  connection,
		Settings:    settings,
		SettingsErr: settingsErr,
		Project:     project.Label(),
	})
	return nil
}
