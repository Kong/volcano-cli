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
	"github.com/Kong/volcano-cli/internal/localgit"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type connectOptions struct {
	deps   cliruntime.Deps
	gitURL string
	remote string
	// rootDirectory and rootDirectorySet are separate because the bind is a
	// full replace: an omitted flag has to leave an existing root directory
	// alone, while an explicitly empty one resets it to the repository root.
	rootDirectory    string
	rootDirectorySet bool
	yes              bool
	in               io.Reader
	out              io.Writer
}

func newConnect(deps cliruntime.Deps) *cobra.Command {
	var (
		remote        string
		rootDirectory string
		yes           bool
	)
	cmd := &cobra.Command{
		Use:   "connect [git-url]",
		Short: "Connect the current project to a GitHub repository",
		Long: `Connect the current project to a GitHub repository.

With no argument, the repository is read from this directory's Git remotes:
the only remote, or "origin" when there are several. Pass a repository URL to
name one explicitly, or use --remote to pick a remote by name.

Connecting only binds the project. Nothing is created on GitHub, nothing is
pushed, and no token is written to your Git config. To start deploying, push to
the repository's default branch yourself.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gitURL := ""
			if len(args) == 1 {
				if gitURL = strings.TrimSpace(args[0]); gitURL == "" {
					// Falling back to remote discovery here would quietly
					// ignore an unset variable in a script, and would slip
					// past the --remote check below.
					return errors.New("the repository URL is empty")
				}
			}
			// Both say where the repository comes from and the URL wins, so
			// taking the pair would silently ignore what the user asked for.
			if gitURL != "" && cmd.Flags().Changed("remote") {
				return errors.New(
					"--remote cannot be combined with a repository URL: both say where the repository comes from")
			}
			return runConnect(cmd.Context(), connectOptions{
				deps:             deps,
				gitURL:           gitURL,
				remote:           remote,
				rootDirectory:    rootDirectory,
				rootDirectorySet: cmd.Flags().Changed("root-directory"),
				yes:              yes,
				in:               cmd.InOrStdin(),
				out:              cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "Git remote to read the repository from (default: the only remote, or \"origin\")")
	cmd.Flags().StringVar(&rootDirectory, "root-directory", "", "Subdirectory the project builds from (default: the repository root)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runConnect(ctx context.Context, opts connectOptions) error {
	service := gitconnect.NewService(opts.deps)
	// A web URL failure only costs the guidance links, and every error path
	// below still reports what went wrong, so it is not worth failing over.
	webURL, _ := service.WebURL()

	repository, explicit, err := resolveRepository(ctx, opts)
	if err != nil {
		return err
	}

	existing, err := currentConnection(ctx, service)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	// Nothing to change: report the binding and stop. Connecting is idempotent
	// on purpose — an agent or a CI job re-running it should not have to
	// special-case "already done".
	if unchanged(existing, repository, opts) {
		settings, _ := service.DeploySettings(ctx)
		output.GitConnection(opts.out, existing, settings)
		return nil
	}

	// Resolve before asking anything. Only the repository id says whether this
	// is really a replacement — repo_full_name is cached at connect time, so a
	// repository renamed on GitHub would otherwise be reported as a different
	// one — and it also means the user is never asked to confirm a binding the
	// CLI has not yet established it can make.
	target, err := service.Resolve(ctx, repository)
	if err != nil {
		return resolveError(opts.deps, webURL, existing, repository, err)
	}

	// Editing the root directory of the repository already bound is not a
	// replacement, and neither is picking up a rename. Neither may be described
	// as one.
	replacing := existing != nil && existing.RepoId != target.Repository.Id
	if replacing {
		replace, err := confirmReplace(opts, existing, target.Repository.FullName)
		if err != nil || !replace {
			return err
		}
	}

	// Confirm an explicitly named repository: the user may be pointing somewhere
	// other than the checkout they are standing in. A replacement was already
	// confirmed above, so it is not asked twice.
	if explicit && existing == nil {
		confirmed, err := confirmConnect(opts, target.Repository.FullName)
		if err != nil || !confirmed {
			return err
		}
	}

	if replacing {
		// The bind is a full replace, so the old binding does not need removing
		// first. Say what happened anyway: the previous repository stops
		// deploying, and that is worth stating rather than leaving to inference.
		output.Note(opts.out, "Replacing the existing connection to %s.", existing.RepoFullName)
	}

	connection, err := service.Connect(ctx, *target, rootDirectoryFor(existing, target, opts))
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	// The binding is made at this point, so failing to read back what a push
	// deploys must not turn a successful connect into an error.
	settings, _ := service.DeploySettings(ctx)
	output.GitConnected(opts.out, connection, settings)
	return nil
}

// resolveRepository determines which repository to connect, and reports whether
// the user named it explicitly rather than it being discovered locally.
func resolveRepository(ctx context.Context, opts connectOptions) (repository localgit.Repository, explicit bool, err error) {
	if opts.gitURL != "" {
		repository, err = localgit.ParseGitHubRepository(opts.gitURL)
		return repository, true, err
	}

	remotes, err := localgit.New(opts.deps).Remotes(ctx)
	if err != nil {
		return localgit.Repository{}, false, err
	}

	remote, err := localgit.SelectRemote(remotes, opts.remote)
	if err != nil {
		return localgit.Repository{}, false, err
	}

	repository, err = localgit.ParseGitHubRepository(remote.URL)
	if err != nil {
		return localgit.Repository{}, false, fmt.Errorf("remote %q: %w", remote.Name, err)
	}
	return repository, false, nil
}

// currentConnection reads the project's binding, mapping "no binding" to a nil
// connection so callers branch on presence rather than on an error.
func currentConnection(ctx context.Context, service gitconnect.Service) (*apiclient.ProjectGitConnection, error) {
	existing, err := service.Status(ctx)
	if err != nil {
		if errors.Is(err, gitconnect.ErrNotConnected) {
			return nil, nil //nolint:nilnil // no binding is an outcome here, not a failure
		}
		return nil, err
	}
	return existing, nil
}

// unchanged reports whether the project is already bound exactly as asked, so
// there is nothing to send and none of the lookups behind Resolve are worth
// making. It compares names because that is all that is known this early; a
// rename therefore falls through to the bind, which refreshes the cached name.
// A root directory the user named explicitly counts: keying only on the
// repository would drop --root-directory in silence, and this bind is the only
// way to edit it.
func unchanged(existing *apiclient.ProjectGitConnection, repository localgit.Repository, opts connectOptions) bool {
	if existing == nil || !sameRepository(existing, repository) {
		return false
	}
	if !opts.rootDirectorySet {
		return true
	}
	return strings.TrimSpace(opts.rootDirectory) == strings.TrimSpace(existing.RootDirectory)
}

// sameRepository compares full names case-insensitively: GitHub preserves the
// case an owner typed but does not treat it as significant.
func sameRepository(existing *apiclient.ProjectGitConnection, repository localgit.Repository) bool {
	return strings.EqualFold(existing.RepoFullName, repository.FullName())
}

// rootDirectoryFor decides what the full-replace bind carries. An omitted flag
// keeps what the same repository already had — the rename case reaches here —
// rather than resetting it the way a bare replace would. A different repository
// starts from the root: the old subdirectory means nothing there.
func rootDirectoryFor(existing *apiclient.ProjectGitConnection, target *gitconnect.Target, opts connectOptions) string {
	switch {
	case opts.rootDirectorySet:
		return opts.rootDirectory
	case existing != nil && existing.RepoId == target.Repository.Id:
		return existing.RootDirectory
	default:
		return ""
	}
}

func confirmReplace(opts connectOptions, existing *apiclient.ProjectGitConnection, wanted string) (bool, error) {
	warning := fmt.Sprintf(
		"This project is already connected to %s. Pushes to it will stop deploying.", existing.RepoFullName)
	// The bind is a full replace and the new repository has no equivalent of
	// the old subdirectory, so it resets. Say so while the user can still
	// decline: it decides what gets built.
	if root := strings.TrimSpace(existing.RootDirectory); root != "" && !opts.rootDirectorySet {
		warning += fmt.Sprintf("\nThe root directory %q will reset to the repository root.", root)
	}
	return ask(opts.in, opts.out, opts.yes, warning, fmt.Sprintf("Replace it with %s?", wanted))
}

func confirmConnect(opts connectOptions, wanted string) (bool, error) {
	return ask(opts.in, opts.out, opts.yes,
		fmt.Sprintf("This will connect the current project to %s.", wanted), "Connect it?")
}

// resolveError explains the one resolve failure a user can act on — the App
// cannot see the repository — and hands everything else to guide.
func resolveError(
	deps cliruntime.Deps, webURL string,
	existing *apiclient.ProjectGitConnection, repository localgit.Repository, err error,
) error {
	if !errors.Is(err, gitconnect.ErrRepositoryNotAccessible) {
		return guide(deps, webURL, err)
	}

	cause := fmt.Sprintf(
		"Either the Volcano GitHub App is not installed on %s, or it is installed for "+
			"selected repositories that do not include this one.", repository.Owner)
	// A project that is already bound has a second, likelier explanation: the
	// repository was renamed and the local remote still names the old one. git
	// keeps working through GitHub's redirect, so a stale remote is easy to
	// carry for a long time without noticing.
	if existing != nil {
		cause += fmt.Sprintf(
			"\nThis project is connected to %s, so the local remote may also simply be out of date.",
			existing.RepoFullName)
	}
	return fmt.Errorf("%w\n\n%s\n\n%s", err, cause,
		dashboardStep(webURL, "Grant it access, then run this command again:"))
}
