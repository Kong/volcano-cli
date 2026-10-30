package git

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type reposOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newRepos(deps cliruntime.Deps) *cobra.Command {
	opts := reposOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "List the GitHub repositories you can connect a project to",
		Long: `List the GitHub repositories you can connect a project to, grouped by the
Volcano GitHub App installation that grants access to each.

An installation scoped to selected repositories lists only those. A repository
marked empty has no commits yet, so it can receive the project's source with
'volcano git export'.

This reads your own GitHub connection, so it needs an account token rather
than a project access token.`,
		Example: "  volcano git repos\n  volcano git repos --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runRepos(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRepos(ctx context.Context, opts reposOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	installations, err := service.Repositories(ctx)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	listings := make([]output.GitInstallationListing, 0, len(installations))
	for _, installation := range installations {
		listings = append(listings, output.GitInstallationListing{
			GitHubAccount:       installation.Connection.ProviderLogin,
			ID:                  installation.Installation.Id,
			AccountLogin:        installation.Installation.AccountLogin,
			AccountType:         installation.Installation.AccountType,
			RepositorySelection: installation.Installation.RepositorySelection,
			Repositories:        installation.Repositories,
		})
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, listings)
	}
	output.GitRepositories(opts.out, listings, cliruntime.CommandPath(opts.deps, "git connect"))
	return nil
}
