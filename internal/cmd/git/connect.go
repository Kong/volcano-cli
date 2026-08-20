package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
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

With no argument, the repository is read from this directory's Git config:
wherever "git push" sends this branch, then the only remote, then "origin".
In a fork or triangular checkout that is not origin, and the CLI says which
setting decided it. Pass a repository URL to name one explicitly, or use
--remote to pick a remote by name.

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
			// An explicitly empty --root-directory resets to the repository
			// root, so it has to stay accepted — but whitespace is a mistyped
			// value or an unset variable, never a request to clear.
			if raw := cmd.Flags().Lookup("root-directory").Value.String(); raw != "" &&
				strings.TrimSpace(raw) == "" {
				return errors.New("--root-directory is only whitespace; pass an empty value to reset it to the repository root")
			}
			// git rejects a remote name containing whitespace, so this is a
			// mistyped value or an unset variable — and it used to do two wrong
			// things at once: naming no remote, while still counting as "the user
			// named one" and suppressing the push routing. The origin convention
			// then decided, and reported success for the wrong repository.
			if raw := cmd.Flags().Lookup("remote").Value.String(); raw != "" &&
				strings.TrimSpace(raw) == "" {
				return errors.New("--remote is only whitespace; pass the name of a Git remote")
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
				rootDirectory:    strings.TrimSpace(rootDirectory),
				rootDirectorySet: cmd.Flags().Changed("root-directory"),
				yes:              yes,
				in:               cmd.InOrStdin(),
				out:              cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "",
		"Git remote to read the repository from (default: where git pushes this branch, else the only remote or \"origin\")")
	cmd.Flags().StringVar(&rootDirectory, "root-directory", "", "Subdirectory the project builds from (default: the repository root)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runConnect(ctx context.Context, opts connectOptions) error {
	service := gitconnect.NewService(opts.deps)
	// A web URL failure only costs the guidance links, and every error path
	// below still reports what went wrong, so it is not worth failing over.
	webURL, _ := service.WebURL()

	if err := validateRootDirectory(opts); err != nil {
		return err
	}

	repository, selected, err := resolveRepository(ctx, opts)
	if err != nil {
		return err
	}
	if selected != nil && selected.source != "" {
		// The pick did not come from the usual convention, so say what decided
		// it. Binding a repository the user never pushes to is the failure this
		// prevents, and silence would make it look like the convention held.
		if selected.remote.Named() {
			output.Note(opts.out, "Using remote %q: %s sends this branch's pushes there.",
				selected.remote.Name, selected.source)
		} else {
			// No remote to name — the configuration holds the URL itself, which
			// is a shape git allows and nothing in `git remote -v` shows.
			pushURL, _ := selected.remote.PushTarget()
			output.Note(opts.out, "Using %s: it sends this branch's pushes to %s.",
				selected.source, localgit.Redact(pushURL))
		}
	}
	if selected != nil && selected.remote.Diverges() {
		// The push URL is the one bound, because a push is what deploys. Say so:
		// the remote the user thinks of as "origin" fetches from somewhere else.
		pushURL, _ := selected.remote.PushTarget()
		output.Note(opts.out, "Remote %q pushes to %s and fetches from %s; the push target is what deploys.",
			selected.remote.Name, localgit.Redact(pushURL), localgit.Redact(selected.remote.FetchURL))
	}

	project, err := service.Project()
	if err != nil {
		return err
	}

	existing, err := currentConnection(ctx, service)
	if err != nil {
		return err
	}

	// Resolve before deciding anything. repo_full_name is cached at connect
	// time, so names cannot answer either question that follows: whether the
	// project is already bound to this repository, or whether binding it
	// replaces another. Only the repository id can, and it takes these lookups
	// to get one. That costs an idempotent re-run three reads, which is worth
	// paying — a name match against a different id would otherwise report a
	// binding that does not exist, and GitHub frees a renamed repository's name
	// for reuse. Resolving first also means the user is never asked to confirm a
	// binding the CLI has not established it can make.
	target, err := service.Resolve(ctx, repository)
	if err != nil {
		return resolveError(opts.deps, webURL, existing, repository, err)
	}

	// Nothing to change: report the binding and stop. Connecting is idempotent
	// on purpose — an agent or a CI job re-running it should not have to
	// special-case "already done".
	if unchanged(existing, target, opts) {
		settings, settingsErr := service.DeploySettings(ctx)
		output.GitConnection(opts.out, output.GitBinding{
			Connection:          existing,
			Settings:            settings,
			SettingsErr:         settingsErr,
			Project:             project.Label(),
			GitHubAccount:       target.ConnectionLogin,
			InstallationAccount: target.InstallationAccount,
		})
		return nil
	}

	// Editing the root directory of the repository already bound is not a
	// replacement, and neither is picking up a rename. Neither may be described
	// as one.
	replacing := existing != nil && existing.RepoId != target.Repository.Id
	if replacing {
		replace, err := confirmReplace(opts, project, existing, target.Repository.FullName)
		if err != nil || !replace {
			return err
		}
	}

	// Confirm an explicitly named repository: the user may be pointing somewhere
	// other than the checkout they are standing in. A replacement was already
	// confirmed above, so it is not asked twice.
	if opts.gitURL != "" && existing == nil {
		confirmed, err := confirmConnect(opts, project, target.Repository.FullName)
		if err != nil || !confirmed {
			return err
		}
	}

	connection, err := service.Connect(ctx, *target, rootDirectoryFor(existing, target, opts), existing)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	if replacing {
		// Said after the write, not before: the write can still be refused, and
		// a past-tense claim next to a failure reads as a contradiction. The
		// bind is a full replace, so the old binding needed no removing — worth
		// stating rather than leaving to inference that it stopped deploying.
		output.Note(opts.out, "Replaced the existing connection to %s.", existing.RepoFullName)
	}

	// The binding is made at this point, so failing to read back what a push
	// deploys must not turn a successful connect into an error — but it is said
	// out loud rather than left to look like "nothing is configured".
	settings, settingsErr := service.DeploySettings(ctx)
	output.GitConnected(opts.out, output.GitBinding{
		Connection:          connection,
		Settings:            settings,
		SettingsErr:         settingsErr,
		Project:             project.Label(),
		GitHubAccount:       target.ConnectionLogin,
		InstallationAccount: target.InstallationAccount,
	})
	return nil
}

// validateRootDirectory refuses what the flag cannot mean. The platform builds
// from this path inside the repository, so an absolute path or one climbing out
// of it deploys nothing — and the CLI is what reports success, so it should not
// report it for a value that cannot work.
func validateRootDirectory(opts connectOptions) error {
	if !opts.rootDirectorySet || opts.rootDirectory == "" {
		return nil
	}

	root := filepath.ToSlash(opts.rootDirectory)
	if strings.HasPrefix(root, "/") || filepath.IsAbs(opts.rootDirectory) {
		return fmt.Errorf("--root-directory must be a path inside the repository, not an absolute path: %s", root)
	}
	for segment := range strings.SplitSeq(root, "/") {
		if segment == ".." {
			return fmt.Errorf("--root-directory must stay inside the repository, so it cannot contain %q: %s", "..", root)
		}
	}
	return nil
}

// selectedRemote is the destination a repository was read from, and the git
// config key that chose it — empty when the "origin" convention did.
type selectedRemote struct {
	remote localgit.Remote
	source string
}

// resolveRepository determines which repository to connect: the one the user
// named, or the one this directory's remotes push to. It returns the remote it
// read when there is something about that worth reporting.
func resolveRepository(
	ctx context.Context, opts connectOptions,
) (repository localgit.Repository, selected *selectedRemote, err error) {
	if opts.gitURL != "" {
		repository, err = localgit.ParseGitHubRepository(opts.gitURL)
		return repository, nil, err
	}

	client := localgit.New(opts.deps)
	remotes, err := client.Remotes(ctx)
	if err != nil {
		return localgit.Repository{}, nil, err
	}

	// Ask git where a push goes rather than assuming origin: a fork or
	// triangular checkout routinely pushes somewhere else, and the repository
	// that receives pushes is the one a deployment comes from.
	//
	// Not asked at all when --remote named one. It outranks the routing anyway,
	// and reading it regardless made an unusable route refuse the very command
	// its own error message offers as the way out of it.
	var push localgit.PushRemote
	if opts.remote == "" {
		if push, err = client.PushRemote(ctx); err != nil {
			return localgit.Repository{}, nil, err
		}
	}

	remote, err := localgit.SelectRemote(remotes, opts.remote, push)
	if err != nil {
		return localgit.Repository{}, nil, err
	}

	pushURL, err := remote.PushTarget()
	if err != nil {
		return localgit.Repository{}, nil, err
	}

	// An unnamed destination came from a URL SelectRemote already parsed, so
	// only a named remote's URL can fail here.
	repository, err = localgit.ParseGitHubRepository(pushURL)
	if err != nil {
		return localgit.Repository{}, nil, fmt.Errorf("remote %q: %w", remote.Name, err)
	}

	// The source is only worth reporting when it changed the answer: a
	// pushDefault of origin picks what the convention would have picked anyway.
	source := ""
	if opts.remote == "" && push.Name != "" && push.Name != localgit.DefaultRemoteName {
		source = push.Source
	}
	return repository, &selectedRemote{remote: remote, source: source}, nil
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
// there is nothing to send. The name has to match as well as the id: an id match
// under a stale cached name means the repository was renamed, and rebinding is
// what refreshes that name. A root directory the user named explicitly counts
// too — keying only on the repository would drop --root-directory in silence,
// and this bind is the only way to edit it.
func unchanged(existing *apiclient.ProjectGitConnection, target *gitconnect.Target, opts connectOptions) bool {
	if existing == nil || existing.RepoId != target.Repository.Id {
		return false
	}
	// The installation is part of the binding: uninstalling and reinstalling the
	// App issues a new id, which leaves the stored one pointing at nothing and
	// no push deploying. Rebinding repairs that, so it is not "unchanged".
	if existing.RepoInstallationId != target.InstallationID {
		return false
	}
	// GitHub preserves the case an owner typed but does not treat it as
	// significant, so a differently-cased name is the same name.
	if !strings.EqualFold(existing.RepoFullName, target.Repository.FullName) {
		return false
	}
	// The bind re-resolves the production branch from GitHub's live default, so
	// a repository whose default moved has a stale one recorded — and that run
	// is exactly the one an operator needs told, not reported as unchanged.
	if existing.ProductionBranch != target.Repository.DefaultBranch {
		return false
	}
	if !opts.rootDirectorySet {
		return true
	}
	return opts.rootDirectory == strings.TrimSpace(existing.RootDirectory)
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

func confirmReplace(
	opts connectOptions, project gitconnect.ProjectRef,
	existing *apiclient.ProjectGitConnection, wanted string,
) (bool, error) {
	warning := fmt.Sprintf(
		"Project %s is already connected to %s. Pushes to it will stop deploying.",
		project.Label(), existing.RepoFullName)
	// The bind is a full replace and the new repository has no equivalent of
	// the old subdirectory, so it resets. Say so while the user can still
	// decline: it decides what gets built.
	if root := strings.TrimSpace(existing.RootDirectory); root != "" && !opts.rootDirectorySet {
		warning += fmt.Sprintf("\nThe root directory %q will reset to the repository root.", root)
	}
	return ask(opts.in, opts.out, opts.yes, warning, fmt.Sprintf("Replace it with %s?", wanted))
}

func confirmConnect(opts connectOptions, project gitconnect.ProjectRef, wanted string) (bool, error) {
	return ask(opts.in, opts.out, opts.yes,
		fmt.Sprintf("This will connect project %s to %s.", project.Label(), wanted), "Connect it?")
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
