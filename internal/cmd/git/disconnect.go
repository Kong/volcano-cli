package git

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type disconnectOptions struct {
	deps cliruntime.Deps
	yes  bool
	in   io.Reader
	out  io.Writer
}

func newDisconnect(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "disconnect",
		Short: "Disconnect the current project from its Git repository",
		Long: `Disconnect the current project from its Git repository.

Only the binding is removed. The repository itself is untouched, and so is the
Volcano GitHub App's access to it. Pushes simply stop deploying.

By default this command prompts for confirmation. Use --yes to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDisconnect(cmd.Context(), disconnectOptions{
				deps: deps,
				yes:  yes,
				in:   cmd.InOrStdin(),
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDisconnect(ctx context.Context, opts disconnectOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	// Read the binding first so the prompt can name the repository, and so a
	// project with nothing connected says exactly that instead of reporting a
	// 404 from the delete.
	existing, err := service.Status(ctx)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	project, err := service.Project()
	if err != nil {
		return err
	}

	confirmed, err := ask(opts.in, opts.out, opts.yes,
		fmt.Sprintf("Pushes to %s will stop deploying project %s. The repository itself is not changed.",
			existing.RepoFullName, project.Label()),
		fmt.Sprintf("Disconnect %s?", existing.RepoFullName))
	if err != nil || !confirmed {
		return err
	}

	if err := service.Disconnect(ctx, existing); err != nil {
		return guide(opts.deps, webURL, err)
	}

	output.GitDisconnected(opts.out, existing.RepoFullName)
	return nil
}
