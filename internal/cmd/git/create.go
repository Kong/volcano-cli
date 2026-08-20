package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/localgit"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps cliruntime.Deps
	// name is empty when the working directory's name should be used.
	name          string
	owner         string
	private       bool
	public        bool
	description   string
	rootDirectory string
	// branch overrides the branch to deploy from; branchSet distinguishes an
	// unset flag from one that was given.
	branch    string
	branchSet bool
	remote    string
	noPush    bool
	ssh       bool
	yes       bool
	in        io.Reader
	out       io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	opts := createOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a GitHub repository for the current project and push to it",
		Long: `Create a new, empty GitHub repository, connect the current project to it, and
push this checkout into it so the project starts deploying on every push.

With no argument the repository takes this directory's name. The repository is
private unless --public, and is created empty: its first commit is the one you
already have here, pushed with your own Git credentials. Volcano never mints or
stores a push credential, and no token is written to your Git config.

Creating a repository cannot be undone from here — Volcano has no way to delete
one — so this asks before it creates, and reports the repository by name if
anything afterwards fails.

To connect a repository that already exists, use "git connect" instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				// Falling back to the directory name here would quietly ignore an
				// unset variable in a script and create a repository under a name
				// the caller never chose.
				if opts.name = strings.TrimSpace(args[0]); opts.name == "" {
					return errors.New("the repository name is empty")
				}
			}
			opts.branchSet = cmd.Flags().Changed("branch")
			opts.in, opts.out = cmd.InOrStdin(), cmd.OutOrStdout()
			return runCreate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.owner, "owner", "",
		"GitHub account to create the repository under (default: your own account)")
	cmd.Flags().BoolVar(&opts.private, "private", false, "Create a private repository (the default)")
	cmd.Flags().BoolVar(&opts.public, "public", false, "Create a public repository")
	cmd.Flags().StringVar(&opts.description, "description", "", "Repository description shown on GitHub")
	cmd.Flags().StringVar(&opts.rootDirectory, "root-directory", "",
		"Subdirectory the project builds from (default: the repository root)")
	cmd.Flags().StringVar(&opts.branch, "branch", "",
		"Branch to deploy from (default: the branch this checkout is on)")
	cmd.Flags().StringVar(&opts.remote, "remote", localgit.DefaultRemoteName,
		"Name for the Git remote added for the new repository")
	cmd.Flags().BoolVar(&opts.noPush, "no-push", false,
		"Create and connect the repository without touching this checkout")
	cmd.Flags().BoolVar(&opts.ssh, "ssh", false, "Record the remote with its ssh URL instead of https")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	request, err := buildCreateRequest(ctx, opts)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	project, err := service.Project()
	if err != nil {
		return err
	}

	// Read the binding before creating anything. The platform refuses a create
	// for a connected project too, but only after the repository exists in one of
	// the two races it guards — so the cheap read here is what keeps the ordinary
	// mistake ("this project already has a repo") from costing one.
	existing, err := currentConnection(ctx, service)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}
	if existing != nil {
		return fmt.Errorf(
			"project %s is already connected to %s\n\nDisconnect it first with %s, or connect a different "+
				"existing repository with %s",
			project.Label(), existing.RepoFullName,
			cliruntime.CommandPath(opts.deps, "git disconnect"),
			cliruntime.CommandPath(opts.deps, "git connect"))
	}

	// Last of the checks, so the prompt is the only thing left between here and a
	// repository: the user is never asked to confirm a create the CLI has already
	// established it cannot make.
	if err := service.CheckOwner(ctx, request.input.Owner); err != nil {
		return guide(opts.deps, webURL, err)
	}

	confirmed, err := confirmCreate(opts, project, request)
	if err != nil || !confirmed {
		return err
	}

	created, err := service.CreateRepository(ctx, request.input)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	return reportCreated(ctx, opts, service, project, request, created)
}

// createRequest is a validated request: everything the platform is asked for,
// plus the local work that follows a successful create.
type createRequest struct {
	input gitconnect.CreateRepositoryInput
	// pushBranch is the branch to push, empty when the checkout is not being
	// touched. It always equals the production branch when set: pushing anything
	// else would leave the project connected and not deploying.
	pushBranch string
}

// buildCreateRequest resolves and checks everything that can be decided without
// creating anything. Creation is irreversible and Volcano cannot delete a
// repository, so a request that cannot work must fail before it is sent.
func buildCreateRequest(ctx context.Context, opts createOptions) (createRequest, error) {
	if opts.private && opts.public {
		return createRequest{}, errors.New("--private and --public cannot be combined: both say who can see the repository")
	}
	if strings.TrimSpace(opts.remote) == "" {
		return createRequest{}, errors.New("--remote is empty: pass the name to give the new Git remote")
	}
	if err := validateRootDirectory(opts.rootDirectory, opts.rootDirectory != ""); err != nil {
		return createRequest{}, err
	}

	name, err := repositoryName(opts.name)
	if err != nil {
		return createRequest{}, err
	}
	branch, err := requestedBranch(opts)
	if err != nil {
		return createRequest{}, err
	}

	request := createRequest{input: gitconnect.CreateRepositoryInput{
		Name:             name,
		Owner:            strings.TrimSpace(opts.owner),
		Private:          !opts.public,
		Description:      opts.description,
		RootDirectory:    strings.TrimSpace(opts.rootDirectory),
		ProductionBranch: branch,
	}}
	if opts.noPush {
		return request, nil
	}
	return withPushBranch(ctx, opts, request)
}

// withPushBranch resolves the branch this command will push and checks the
// checkout can push it.
//
// All of it runs before the repository is created, because a checkout that
// cannot push is a fixable mistake before the create and an orphaned repository
// after it.
func withPushBranch(ctx context.Context, opts createOptions, request createRequest) (createRequest, error) {
	client := localgit.New(opts.deps)
	checkout, err := client.Checkout(ctx)
	if err != nil {
		return createRequest{}, fmt.Errorf(
			"%w\n\nCommit what you want to push first, or pass --no-push to create the repository "+
				"and push it yourself", err)
	}
	if !checkout.HasCommits {
		return createRequest{}, errors.New(
			"this repository has no commits, so there is nothing to push\n\n" +
				"Commit your work first, or pass --no-push to create the repository and push it yourself")
	}

	branch := request.input.ProductionBranch
	if branch == "" {
		// The whole reason this command resolves a branch at all: a repository
		// created empty has no default branch, so the platform would have to
		// predict one, and a wrong prediction leaves the project connected and
		// never deploying.
		if checkout.Branch == "" {
			return createRequest{}, errors.New(
				"this checkout is not on a branch, so there is no branch to deploy from\n\n" +
					"Check out a branch, or name one with --branch")
		}
		branch = checkout.Branch
		request.input.ProductionBranch = branch
	}

	// A named branch that does not exist locally cannot be pushed, and pushing
	// something else would deploy nothing.
	if branch != checkout.Branch {
		exists, err := client.BranchExists(ctx, branch)
		if err != nil {
			return createRequest{}, err
		}
		if !exists {
			return createRequest{}, fmt.Errorf(
				"--branch %s does not exist in this checkout, so it cannot be pushed\n\n"+
					"Create it, name the branch you are on (%s), or pass --no-push", branch, checkout.Branch)
		}
	}

	if err := checkRemoteFree(ctx, client, opts.remote); err != nil {
		return createRequest{}, err
	}
	request.pushBranch = branch
	return request, nil
}

// checkRemoteFree refuses a remote name this checkout already uses. git refuses
// it too, but only after the repository exists; and taking the name over would
// silently redirect where this checkout already pushes.
func checkRemoteFree(ctx context.Context, client localgit.Client, name string) error {
	remotes, err := client.Remotes(ctx)
	if err != nil {
		return err
	}
	for _, remote := range remotes {
		if remote.Name != name {
			continue
		}
		return fmt.Errorf(
			"this checkout already has a remote named %q\n\n"+
				"Pass --remote with another name, or --no-push to leave the checkout alone", name)
	}
	return nil
}

// repositoryName resolves the name to create under, defaulting to the working
// directory's own name.
func repositoryName(named string) (string, error) {
	name := named
	if name == "" {
		working, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not read the working directory to name the repository: %w", err)
		}
		name = filepath.Base(working)
	}
	if err := checkRepositoryName(name); err != nil {
		return "", err
	}
	return name, nil
}

// checkRepositoryName refuses a name GitHub would not create as written.
//
// GitHub does not reject most of these: it silently replaces the characters it
// does not allow, so a directory named "my app" becomes "my-app" — a repository
// under a name nobody chose, bound to the project, and reported by this command
// as if it were the name asked for. Refusing is the only outcome that keeps what
// the CLI reports true.
func checkRepositoryName(name string) error {
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return fmt.Errorf("%q cannot be used as a repository name: pass one as an argument", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf(
				"repository name %q contains %q, which GitHub does not allow: use letters, digits, "+
					"%q, %q or %q", name, r, "-", "_", ".")
		}
	}
	return nil
}

// requestedBranch reads the --branch flag, refusing only what would break this
// command's own git invocation or is plainly a mistyped value. The platform
// applies git's full ref-name rules before it creates anything, and duplicating
// them here would only add a second opinion to disagree with.
func requestedBranch(opts createOptions) (string, error) {
	if !opts.branchSet {
		return "", nil
	}
	branch := opts.branch
	if strings.TrimSpace(branch) == "" {
		return "", errors.New("--branch is empty: pass the branch a push should deploy from")
	}
	if branch != strings.TrimSpace(branch) {
		return "", fmt.Errorf("--branch %q is padded with whitespace, which is not part of a branch name", branch)
	}
	if strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("--branch %q cannot start with %q: git reads it as an option", branch, "-")
	}
	return branch, nil
}

func confirmCreate(opts createOptions, project gitconnect.ProjectRef, request createRequest) (bool, error) {
	visibility := "private"
	if !request.input.Private {
		visibility = "public"
	}
	owner := request.input.Owner
	if owner == "" {
		owner = "your GitHub account"
	}
	warning := fmt.Sprintf(
		"This creates a new %s repository %q under %s and connects project %s to it.\n"+
			"Volcano cannot delete a repository, so this cannot be undone from here.",
		visibility, request.input.Name, owner, project.Label())
	question := "Create it?"
	if request.pushBranch != "" {
		question = fmt.Sprintf("Create it and push %s?", request.pushBranch)
	}
	return ask(opts.in, opts.out, opts.yes, warning, question)
}

// reportCreated finishes the local half and reports the outcome. The repository
// and the binding both exist by now, so nothing here may return an error that
// reads as "the repository was not created".
func reportCreated(
	ctx context.Context,
	opts createOptions,
	service gitconnect.Service,
	project gitconnect.ProjectRef,
	request createRequest,
	created *apiclient.CreatedProjectGitConnection,
) error {
	connection := &created.Connection
	local, err := pushToCreated(ctx, opts, request, created)

	settings, settingsErr := service.DeploySettings(ctx)
	output.GitCreated(opts.out, output.GitCreation{
		Binding: output.GitBinding{
			Connection:  connection,
			Settings:    settings,
			SettingsErr: settingsErr,
			Project:     project.Label(),
		},
		AppInstalled: created.AppInstalled,
		InstallURL:   created.InstallUrl,
		RemoteAdded:  local.remoteAdded,
		RemoteName:   opts.remote,
		Pushed:       local.pushed,
		NextCommands: nextCommands(opts, created, local),
	})
	if err != nil {
		// The repository exists and the project is bound to it. Saying only that
		// the push failed would read as "nothing happened", so the name goes in
		// the error too — this is the same rule the platform applies to every
		// post-creation failure.
		return fmt.Errorf("%s was created and connected, but the local step did not finish: %w",
			connection.RepoFullName, err)
	}
	return nil
}

// localWork records what was done to the checkout, so the report describes the
// state the user is actually left in rather than what was intended.
type localWork struct {
	remoteAdded bool
	pushed      bool
}

// pushToCreated adds the remote and pushes.
//
// The push is skipped when the App cannot see the repository: it would succeed
// and deploy nothing, which is the one failure in this flow that produces no
// error anywhere. The remote is still recorded, because it is what the user
// needs to push by hand once access is granted.
func pushToCreated(
	ctx context.Context, opts createOptions, request createRequest, created *apiclient.CreatedProjectGitConnection,
) (localWork, error) {
	if request.pushBranch == "" {
		return localWork{}, nil
	}

	client := localgit.New(opts.deps)
	if err := client.AddRemote(ctx, opts.remote, remoteURL(opts, created.Connection.RepoFullName)); err != nil {
		return localWork{}, err
	}
	done := localWork{remoteAdded: true}
	if !created.AppInstalled {
		return done, nil
	}
	if err := client.Push(ctx, opts.out, opts.remote, request.pushBranch); err != nil {
		return done, err
	}
	done.pushed = true
	return done, nil
}

func remoteURL(opts createOptions, fullName string) string {
	if opts.ssh {
		return localgit.SSHRemoteURL(fullName)
	}
	return localgit.HTTPSRemoteURL(fullName)
}

// nextCommands is what the user has to run to finish, starting from whatever was
// not already done for them.
//
// The branch comes from the binding rather than from the request: with --no-push
// nothing local was resolved, and the platform's answer is the branch a push has
// to land on — naming any other one here would print a command that deploys
// nothing.
func nextCommands(opts createOptions, created *apiclient.CreatedProjectGitConnection, done localWork) []string {
	commands := make([]string, 0, 2)
	if !done.remoteAdded {
		commands = append(commands,
			fmt.Sprintf("git remote add %s %s", opts.remote, remoteURL(opts, created.Connection.RepoFullName)))
	}
	return append(commands,
		fmt.Sprintf("git push --set-upstream %s %s", opts.remote, created.Connection.ProductionBranch))
}
