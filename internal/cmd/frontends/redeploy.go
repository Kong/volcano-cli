package frontends

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type redeployOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
}

func newRedeploy(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "redeploy <name-or-id>",
		Short: "Redeploy a frontend",
		Long: `Start a new deployment using the latest uploaded frontend archive.

This does not re-upload the local project folder. If the frontend was deployed
with --app-root, the stored app root is reused.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRedeploy(cmd.Context(), redeployOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func runRedeploy(ctx context.Context, opts redeployOptions) error {
	frontend, err := clifrontend.NewService(opts.deps).Redeploy(ctx, opts.identifier)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Frontend '%s' redeploy started", frontend.Name)
	if frontend.CurrentDeploymentId != nil {
		fmt.Fprintf(opts.out, "Deployment: %s\n", frontend.CurrentDeploymentId.String())
	}
	return nil
}
