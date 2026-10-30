package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type exportOptions struct {
	deps cliruntime.Deps
	// branch is the production branch the caller expects, "" to take the one
	// the project has.
	branch     string
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

// promptWriter is where a confirmation goes: stderr when stdout carries JSON,
// so the question never lands in a document a script parses.
func promptWriter(jsonOutput bool, out, errOut io.Writer) io.Writer {
	if jsonOutput {
		return errOut
	}
	return out
}

func newExport(deps cliruntime.Deps) *cobra.Command {
	opts := exportOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write the project's source to its empty connected repository",
		Long: fmt.Sprintf(`Write the source Volcano stores for the current project to its connected
repository, as the first commit on the production branch, and push it.

The repository must be empty, with no commits and no branches; Volcano never
creates it. An empty repository has no default branch, so connect it naming
one: '%s'. Export runs once per project.

The push deploys like any other push to the production branch. Until that
deployment succeeds, direct deploys of functions and frontends are paused;
'%s' resumes them. Once it succeeds, the repository is the project's source of
truth and code deploys only by pushing.

Export takes each function and frontend from its latest successful deployment.
Resources that never deployed successfully are skipped. Migrations, variable
values and credential-shaped files such as .env are left out. The command lists
both.

Pass --branch to name the production branch you expect: the command refuses if
the project's is different. By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
			cliruntime.CommandPath(deps, "git connect <git-url> --production-branch main"),
			cliruntime.CommandPath(deps, "git export cancel")),
		Example: "  volcano git export\n  volcano git export --branch main --yes --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("branch") && strings.TrimSpace(opts.branch) == "" {
				return errors.New("--branch is empty; name the production branch you expect, such as main")
			}
			opts.branch = strings.TrimSpace(opts.branch)
			opts.in, opts.out, opts.errOut = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return runExport(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Production branch you expect the export to create")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.AddCommand(newExportCancel(deps))
	return cmd
}

func runExport(ctx context.Context, opts exportOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	project, err := service.Project()
	if err != nil {
		return err
	}

	// The state comes first, so an API with no stored source, or a project
	// that has already exported, says so before anything is asked.
	state, err := service.SourceState(ctx)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}
	if err := exportRefusal(opts.deps, state); err != nil {
		return err
	}

	connection, err := service.Status(ctx)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}
	branch := connection.ProductionBranch
	if opts.branch != "" && opts.branch != branch {
		return fmt.Errorf("the production branch of %s is %s, not %s. Nothing was exported\n\n"+
			"Pass --branch %s to export to it, or change it first with %s",
			connection.RepoFullName, branch, opts.branch,
			branch, cliruntime.CommandPath(opts.deps, "git production-branch "+opts.branch))
	}

	confirmed, err := ask(opts.in, promptWriter(opts.jsonOutput, opts.out, opts.errOut), opts.yes,
		fmt.Sprintf("This writes the stored source of project %s to %s as the first commit on %s, and pushes it. "+
			"The push deploys. Once that deployment succeeds, code deploys only by pushing to the repository. "+
			"Export runs once.", project.Label(), connection.RepoFullName, branch),
		fmt.Sprintf("Export to %s?", connection.RepoFullName))
	if err != nil || !confirmed {
		return err
	}

	// The repository and branch shown are the ones sent: the binding is read
	// again, and the platform refuses the export if the branch changed.
	result, err := service.ExportSource(ctx, connection)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, result)
	}
	output.GitSourceExported(opts.out, result, cliruntime.CommandPath(opts.deps, "git status"))
	return nil
}

// exportRefusal refuses, before anything is asked, an export the platform is
// certain to refuse, and names what to do instead. The platform still decides:
// git_exporting passes, because retrying an export whose push GitHub did not
// confirm is how that state is resolved.
func exportRefusal(deps cliruntime.Deps, state *apiclient.ProjectSourceExportState) error {
	switch {
	case state.Mode == apiclient.ProjectSourceExportStateModeGit:
		return errors.New("this project's repository is already its source of truth, so there is nothing to export\n\n" +
			"Push to the production branch to deploy code")
	case state.Mode == apiclient.ProjectSourceExportStateModeGitPending:
		return fmt.Errorf("this project's source has been exported, and its deployment is running\n\n"+
			"Follow it with %s, or cancel it with %s",
			cliruntime.CommandPath(deps, "git status"), cliruntime.CommandPath(deps, "git export cancel"))
	case state.ExportedAt != nil:
		return errors.New("this project's source has already been exported, and export can initialize a repository only once")
	default:
		return nil
	}
}

type exportCancelOptions struct {
	deps       cliruntime.Deps
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newExportCancel(deps cliruntime.Deps) *cobra.Command {
	opts := exportCancelOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel an incomplete source export",
		Long: `Cancel a source export that has not finished, so direct deploys of functions
and frontends work again.

The repository and any commit already pushed to it are not changed. If the
initial commit was already made, export cannot run again. An export whose
deployment has succeeded is finished and cannot be canceled.

By default this command prompts for confirmation. Use --yes to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.in, opts.out, opts.errOut = cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()
			return runExportCancel(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit the project's source state as machine-readable JSON")
	return cmd
}

func runExportCancel(ctx context.Context, opts exportCancelOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	project, err := service.Project()
	if err != nil {
		return err
	}

	state, err := service.SourceState(ctx)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}
	switch state.Mode {
	case apiclient.ProjectSourceExportStateModePlatform:
		return errors.New("this project has no incomplete source export to cancel")
	case apiclient.ProjectSourceExportStateModeGit:
		return errors.New("this project's export has finished: its repository is the source of truth, " +
			"so there is nothing to cancel")
	}

	confirmed, err := ask(opts.in, promptWriter(opts.jsonOutput, opts.out, opts.errOut), opts.yes,
		fmt.Sprintf("Direct deploys of project %s will work again. The repository and anything already pushed to it are not changed.",
			project.Label()),
		"Cancel the source export?")
	if err != nil || !confirmed {
		return err
	}

	if err := service.CancelSourceExport(ctx); err != nil {
		return guide(opts.deps, webURL, err)
	}

	// Read again: whether export can still run depends on how far it got, and
	// only the platform knows that.
	after, err := service.SourceState(ctx)
	if err != nil {
		return fmt.Errorf("canceled the source export, but reading the project's source state afterwards failed: %w\n\n"+
			"Run %s to see it", err, cliruntime.CommandPath(opts.deps, "git status"))
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, after)
	}
	output.GitSourceExportCanceled(opts.out, after.ExportedAt != nil)
	return nil
}
