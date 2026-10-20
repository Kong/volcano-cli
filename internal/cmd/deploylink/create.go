package deploylink

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps       cliruntime.Deps
	input      deploylink.CreateInput
	jsonOutput bool
	out        io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a deploy link for the project's repository",
		Long: `Create a deploy link for the GitHub repository the current project is
connected to, and print its address with Markdown and HTML snippets that show a
deploy button.

--branch is the branch the link follows and defaults to the project's production
branch. --path is a build root inside the repository, such as apps/web, for a
repository that holds several apps. --name is the project name offered to
visitors; without it Volcano chooses one.

Volcano checks that the repository is public, that the connected GitHub account
can push to it, and that the branch holds something to deploy. Nothing is
created when a check fails.`,
		Example: fmt.Sprintf("  %s\n  %s",
			cliruntime.CommandPath(deps, "deploy-link create"),
			cliruntime.CommandPath(deps, "deploy-link create --branch main --path apps/web --name my-app")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runCreate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.input.Branch, "branch", "", "Branch the link follows (default: the project's production branch)")
	cmd.Flags().StringVar(&opts.input.Path, "path", "", "Build root inside the repository, such as apps/web (default: the repository root)")
	cmd.Flags().StringVar(&opts.input.SuggestedName, "name", "", "Project name offered to visitors")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	link, err := deploylink.NewService(opts.deps).Create(ctx, opts.input)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, link)
	}
	output.DeployLinkCreated(opts.out, link)
	return nil
}
