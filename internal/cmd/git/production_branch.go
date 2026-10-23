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
	// branch is the branch to deploy from, "" to follow the repository's
	// default branch.
	branch string
	out    io.Writer
}

func newProductionBranch(deps cliruntime.Deps) *cobra.Command {
	var reset bool
	cmd := &cobra.Command{
		Use:   "production-branch [branch]",
		Short: "Change the branch a push must land on to deploy",
		Long: fmt.Sprintf(`Change the branch of the connected repository that deploys when pushed to. The
repository stays connected as it is.

The branch does not have to exist yet. Naming one makes it the project's own
choice: it stays the production branch if the repository's default branch is
renamed or changed. Pass --reset to follow the repository's default branch
again.

This works with a project access token, so CI or an agent can change it.
Connecting a different repository needs a person's account token: see '%s'.

Nothing is redeployed. The command says what a push to the production branch
deploys.`,
			cliruntime.CommandPath(deps, "git connect")),
		Example: "  volcano git production-branch release\n  volcano git production-branch --reset",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch, err := productionBranchArgument(args, reset)
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
	cmd.Flags().BoolVar(&reset, "reset", false, "Follow the repository's default branch")
	return cmd
}

// productionBranchArgument is the branch asked for. Following the default
// takes --reset rather than an empty argument, which is what an unset variable
// in a script looks like. Whether the name is one git accepts is the API's to
// say: it checks it the same way for every client.
func productionBranchArgument(args []string, reset bool) (string, error) {
	switch {
	case reset && len(args) == 1:
		return "", errors.New("pass a branch or --reset, not both")
	case reset:
		return "", nil
	case len(args) == 0:
		return "", errors.New("name the branch a push must land on to deploy, such as main, " +
			"or pass --reset to follow the repository's default branch")
	}
	branch := strings.TrimSpace(args[0])
	if branch == "" {
		return "", errors.New("the branch is empty; pass --reset to follow the repository's default branch")
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

	// Nothing is read first, as for the root directory: the write names only
	// the branch, so it cannot replace a repository connected meanwhile.
	connection, err := service.SetProductionBranch(ctx, opts.branch)
	if err != nil {
		return guideBindingEdit(ctx, opts.deps, service, webURL, err)
	}

	settings, settingsErr := service.DeploySettings(ctx)
	output.GitProductionBranchSet(opts.out, output.GitBinding{
		Connection:  connection,
		Settings:    settings,
		SettingsErr: settingsErr,
		Project:     project.Label(),
	})
	return nil
}
