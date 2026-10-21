package deploylink

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

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
repository that holds several apps; without it the link uses the root directory
of the project's Git connection, else the repository root. --name is the project
name offered to visitors, so it may hold only letters, numbers, underscores and
hyphens, up to 64 characters; without it Volcano chooses one.

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
	cmd.Flags().StringVar(&opts.input.Path, "path", "", "Build root inside the repository, such as apps/web (default: the Git connection's root directory, else the repository root)")
	cmd.Flags().StringVar(&opts.input.SuggestedName, "name", "", "Project name offered to visitors (letters, numbers, _ and -; up to 64)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	if err := checkSuggestedName(opts.input.SuggestedName); err != nil {
		return err
	}
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

// suggestedNamePattern is what the API accepts as a project name: the name
// becomes the project a visitor creates.
var suggestedNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)

const maxSuggestedNameLength = 64

// checkSuggestedName refuses a --name the API would, so the user reads what is
// wrong before a request is made. An empty name leaves the choice to Volcano.
func checkSuggestedName(name string) error {
	name = strings.TrimSpace(name)
	if len(name) > maxSuggestedNameLength {
		return fmt.Errorf("--name is %d characters; use at most %d", len(name), maxSuggestedNameLength)
	}
	if !suggestedNamePattern.MatchString(name) {
		return fmt.Errorf("invalid --name %q: use only letters, numbers, underscores and hyphens", name)
	}
	return nil
}
