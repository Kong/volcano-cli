package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type productionBranchOptions struct {
	deps cliruntime.Deps
	// branch is the branch to pin, "" to follow the repository's default
	// branch.
	branch string
	out    io.Writer
}

func newProductionBranch(deps cliruntime.Deps) *cobra.Command {
	var followDefault bool
	cmd := &cobra.Command{
		Use:   "production-branch [branch]",
		Short: "Change the branch a push must land on to deploy",
		Long: fmt.Sprintf(`Change the branch of the connected repository that a push must land on to
deploy. The repository stays connected as it is.

A project follows the repository's default branch on GitHub until it names
one: renaming the default branch on GitHub moves it too. Naming a branch pins
the project to it, even when it is the current default, so a later rename no
longer moves it. Pass --follow-default to follow the default branch again.

The branch does not have to exist yet, so a project can point at a branch
that is about to be pushed. '%s' shows which branch a project deploys from,
and whether it follows the default.

This works with a project access token, so CI or an agent can set it. Nothing
is deployed: only pushes to the branch deploy from now on.`,
			cliruntime.CommandPath(deps, "git status")),
		Example: "  volcano git production-branch release\n  volcano git production-branch --follow-default",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch, err := productionBranchArgument(args, followDefault)
			if err != nil {
				return err
			}
			return runProductionBranch(cmd.Context(), productionBranchOptions{
				deps:   deps,
				branch: branch,
				out:    cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&followDefault, "follow-default", false,
		"Follow the repository's default branch, including later renames")
	return cmd
}

// productionBranchArgument is the branch asked for. Following the default
// takes --follow-default rather than an empty argument, which is what an
// unset variable in a script looks like.
func productionBranchArgument(args []string, followDefault bool) (string, error) {
	switch {
	case followDefault && len(args) == 1:
		return "", errors.New("pass a branch or --follow-default, not both")
	case followDefault:
		return "", nil
	case len(args) == 0:
		return "", errors.New("name the branch a push must land on to deploy, such as release, " +
			"or pass --follow-default to follow the repository's default branch")
	}
	branch := strings.TrimSpace(args[0])
	if branch == "" {
		return "", errors.New("the branch is empty; pass --follow-default to follow the repository's default branch")
	}
	return branch, nil
}

func runProductionBranch(ctx context.Context, opts productionBranchOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	project, err := service.Project()
	if err != nil {
		return err
	}

	// Nothing is read first: the write names only the branch, so it cannot
	// replace a repository someone else connected meanwhile.
	connection, err := service.SetProductionBranch(ctx, opts.branch)
	if err != nil {
		// "Connect one" is only advice where a repository can be connected.
		if errors.Is(err, gitconnect.ErrNotConnected) {
			if cannot := service.CanConnect(ctx); cannot != nil {
				err = cannot
			}
		}
		return guide(opts.deps, webURL, err)
	}

	// Read afterwards, like root-directory: the branch is set either way, and
	// only these say whether a push to it deploys anything.
	settings, settingsErr := service.DeploySettings(ctx)
	output.GitProductionBranchSet(opts.out, output.GitBinding{
		Connection:  connection,
		Settings:    settings,
		SettingsErr: settingsErr,
		Project:     project.Label(),
		GitCommand:  cliruntime.CommandPath(opts.deps, "git"),
	})
	return nil
}
