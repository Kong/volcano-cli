package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/localgit"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type exportOptions struct {
	deps cliruntime.Deps
	// name is empty when the working directory's name should be used.
	name          string
	owner         string
	private       bool
	public        bool
	description   string
	rootDirectory string
	// branch is the branch to push, which becomes the branch that deploys. Always
	// given: see the note where the flag is marked required.
	branch string
	remote string
	// repo names an existing repository to export into, instead of creating one.
	repo   string
	noPush bool
	ssh    bool
	yes    bool
	in     io.Reader
	out    io.Writer
}

func newExport(deps cliruntime.Deps) *cobra.Command {
	opts := exportOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "export [name]",
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

Connecting a repository you already have is a dashboard flow; this command only
creates new ones.`,
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
			opts.in, opts.out = cmd.InOrStdin(), cmd.OutOrStdout()
			return runExport(cmd.Context(), opts)
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
		"Branch to push, which becomes the branch that deploys (required)")
	cmd.Flags().StringVar(&opts.repo, "repo", "",
		"Export into a GitHub repository that already exists, instead of creating one")
	cmd.Flags().StringVar(&opts.remote, "remote", localgit.DefaultRemoteName,
		"Name for the Git remote added for the repository")
	cmd.Flags().BoolVar(&opts.noPush, "no-push", false,
		"Create and connect the repository without touching this checkout")
	cmd.Flags().BoolVar(&opts.ssh, "ssh", false, "Record the remote with its ssh URL instead of https")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	// Stated, never inferred. This CLI runs from any terminal and any checkout, so
	// the branch that happens to be checked out is not evidence of the branch the
	// user wants deployed — and inferring it would pin a project to a feature
	// branch for good. git pushes a branch it is not standing on perfectly well,
	// so naming it costs the user nothing but a switch they no longer have to make.
	_ = cmd.MarkFlagRequired("branch")
	return cmd
}

func runExport(ctx context.Context, opts exportOptions) error {
	service := gitconnect.NewService(opts.deps)
	webURL, _ := service.WebURL()

	request, err := buildExportRequest(ctx, opts)
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
		// Both ways out of this are dashboard flows: a project holds one
		// repository, and neither disconnecting it nor pointing it somewhere else
		// is something this CLI does.
		return fmt.Errorf(
			"project %s is already connected to %s\n\n%s",
			project.Label(), existing.RepoFullName,
			dashboardStep(webURL, "Disconnect it there first, or point it at another repository:"))
	}

	// Last of the checks, so the prompt is the only thing left between here and a
	// repository: the user is never asked to confirm a create the CLI has already
	// established it cannot make. This is where a caller with no GitHub account
	// connected is turned away, which is the commonest reason a create cannot work.
	if err := service.CheckCreatable(ctx, request.input.Owner); err != nil {
		return guide(opts.deps, webURL, err)
	}

	confirmed, err := confirmExport(opts, project, request)
	if err != nil || !confirmed {
		return err
	}

	created, err := service.CreateRepository(ctx, request.input)
	if err != nil {
		return guide(opts.deps, webURL, err)
	}

	return reportExport(ctx, opts, service, project, exportOutcome{
		connection: &created.Connection,
		created:    true,
		appMissing: !created.AppInstalled,
		installURL: created.InstallUrl,
		pushBranch: request.pushBranch,
		routing:    request.routing,
	})
}

// exportRequest is a validated request: everything the platform is asked for,
// plus the local work that follows a successful create.
type exportRequest struct {
	input gitconnect.CreateRepositoryInput
	// pushBranch is the branch to push, empty when the checkout is not being
	// touched. It always equals the production branch when set: pushing anything
	// else would leave the project connected and not deploying.
	pushBranch string
	// routing is where this checkout's configuration sends a bare `git push`,
	// when that is not the remote being created.
	routing pushRouting
}

// pushRouting is what a later bare `git push` in this checkout would do.
//
// The create's own push names its remote, so routing never changes where it
// goes. What it changes is everything after: `--set-upstream` writes
// branch.<name>.remote, which git consults *last* — behind
// branch.<name>.pushRemote and remote.pushDefault. A checkout with either of
// those set keeps sending bare pushes to the old destination, so the new
// repository silently stops receiving commits and the project silently stops
// deploying.
type pushRouting struct {
	// Elsewhere is true when the configuration sends pushes somewhere other than
	// the remote this command created.
	Elsewhere bool
	// Target is the remote name or URL it sends them to, redacted.
	Target string
	// Source is the config key that decided it, so the user can fix the one that
	// matters.
	Source string
	// Err is why the routing could not be read, when it could not. A malformed
	// push configuration is not a reason to fail a create, but it is a reason not
	// to claim the routing is fine.
	Err error
}

// buildExportRequest resolves and checks everything that can be decided without
// creating anything. Creation is irreversible and Volcano cannot delete a
// repository, so a request that cannot work must fail before it is sent.
func buildExportRequest(ctx context.Context, opts exportOptions) (exportRequest, error) {
	if opts.private && opts.public {
		return exportRequest{}, errors.New("--private and --public cannot be combined: both say who can see the repository")
	}
	if err := checkRemoteName(opts.remote); err != nil {
		return exportRequest{}, err
	}
	root, err := requestedRootDirectory(opts.rootDirectory)
	if err != nil {
		return exportRequest{}, err
	}

	name, err := repositoryName(opts.name)
	if err != nil {
		return exportRequest{}, err
	}
	branch, err := requestedBranch(opts)
	if err != nil {
		return exportRequest{}, err
	}

	request := exportRequest{input: gitconnect.CreateRepositoryInput{
		Name:             name,
		Owner:            strings.TrimSpace(opts.owner),
		Private:          !opts.public,
		Description:      opts.description,
		RootDirectory:    root,
		ProductionBranch: branch,
	}}
	if opts.noPush {
		return request, nil
	}

	plan, err := prepareCheckout(ctx, opts, branch)
	if err != nil {
		return exportRequest{}, err
	}
	// The branch about to be pushed is the one the new repository deploys from,
	// so it is what the create carries.
	request.input.ProductionBranch = plan.branch
	request.pushBranch, request.routing = plan.branch, plan.routing
	return request, nil
}

// checkoutPlan is the local half of an export, resolved before anything
// irreversible happens.
type checkoutPlan struct {
	// branch is the branch to push, empty when the checkout is not being touched.
	branch  string
	routing pushRouting
}

// prepareCheckout checks the checkout can push the branch the caller named.
//
// It deliberately never reads HEAD. The branch to export is stated, not
// inferred, so which branch happens to be checked out is not this command's
// business — `git push origin <branch>` sends any local branch without touching
// the working tree. Inferring it would mean a user standing on a feature branch
// exports that one, and the project then deploys from it for good.
//
// All of it runs before the repository is created or bound: a checkout that
// cannot push is a fixable mistake beforehand and an orphaned repository after.
func prepareCheckout(ctx context.Context, opts exportOptions, branch string) (checkoutPlan, error) {
	client := localgit.New(opts.deps)
	// A branch ref points at a commit, so this covers "is there anything to
	// push?" as well as "is that branch here?". Outside a repository git fails
	// here, which is the same answer with a better message than any check of ours.
	exists, err := client.BranchExists(ctx, branch)
	if err != nil {
		return checkoutPlan{}, fmt.Errorf(
			"%w\n\nRun this from a checkout that has %s, or pass --no-push to leave the checkout alone",
			err, branch)
	}
	if !exists {
		return checkoutPlan{}, fmt.Errorf(
			"branch %s does not exist in this checkout, so it cannot be pushed\n\n"+
				"Create or fetch it here, name another with --branch, or pass --no-push", branch)
	}

	// Asked of git, and asked here rather than afterwards: git refuses more remote
	// names than the flag check can state, and a refusal on the far side of the
	// create leaves a repository whose remote never got recorded.
	valid, err := client.ValidRemoteName(ctx, opts.remote)
	if err != nil {
		return checkoutPlan{}, err
	}
	if !valid {
		return checkoutPlan{}, fmt.Errorf(
			"git will not accept %q as a remote name\n\nPass --remote with another name", opts.remote)
	}
	if err := checkRemoteFree(ctx, client, opts.remote); err != nil {
		return checkoutPlan{}, err
	}
	return checkoutPlan{branch: branch, routing: readPushRouting(ctx, client, opts.remote, branch)}, nil
}

// readPushRouting reports whether a later bare `git push` would leave the new
// repository behind.
//
// Best-effort on purpose: this describes what happens after the create, so a
// configuration this cannot read is worth saying so about but not worth refusing
// a create over. The value is redacted because a CI rewrite routinely leaves a
// job token in one of these keys.
func readPushRouting(ctx context.Context, client localgit.Client, remote, branch string) pushRouting {
	push, err := client.PushRemote(ctx, branch)
	switch {
	case err != nil:
		return pushRouting{Err: err}
	case push.Name == "" || push.Name == remote:
		// Nothing configured, or configured to the remote being created: after
		// --set-upstream a bare push reaches it either way.
		return pushRouting{}
	default:
		target := push.Name
		if push.RewrittenURL != "" {
			target = push.RewrittenURL
		}
		return pushRouting{Elsewhere: true, Target: localgit.Redact(target), Source: push.Source}
	}
}

// requestedRootDirectory reads --root-directory for a repository being created.
//
// A value that is only whitespace is refused rather than trimmed away. connect
// accepts an empty one because it has a previous value to reset; a repository
// being created has none, so an empty-after-trimming value is a mistyped path or
// an unset variable in a script and nothing else — and it would otherwise pass
// silently, leaving an irreversible create bound to the repository root instead
// of failing.
func requestedRootDirectory(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if value != "" && trimmed == "" {
		return "", errors.New(
			"--root-directory is only whitespace; omit it to build from the repository root")
	}
	if trimmed == "" {
		return "", nil
	}
	// The platform builds from this path inside the repository, so an absolute
	// path or one climbing out of it deploys nothing — and this command is what
	// reports success, so it must not report it for a value that cannot work.
	// Refused before the create, like everything else that is checkable.
	root := filepath.ToSlash(trimmed)
	if strings.HasPrefix(root, "/") || filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("--root-directory must be a path inside the repository, not an absolute path: %s", root)
	}
	for segment := range strings.SplitSeq(root, "/") {
		if segment == ".." {
			return "", fmt.Errorf("--root-directory must stay inside the repository, so it cannot contain %q: %s",
				"..", root)
		}
	}
	return trimmed, nil
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

// checkRemoteName refuses the two remote names git cannot be asked about: an
// empty one, and one starting with "-", which git's own validator would read as
// an option instead of a name. Whitespace is refused here too, because that is
// the mistyped value or unset variable this catches in a script, and saying so
// beats "git will not accept it".
//
// Everything else is left to ValidRemoteName, which asks git. This check runs
// even for --no-push, where nothing local happens: the report still prints a
// `git remote add` for the user to run, and it should be a command that works.
func checkRemoteName(name string) error {
	switch {
	case name == "":
		return errors.New("--remote is empty: pass the name to give the new Git remote")
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("--remote %q cannot start with %q: git reads it as an option", name, "-")
	case strings.ContainsFunc(name, unicode.IsSpace):
		return fmt.Errorf("--remote %q contains whitespace, which git does not allow in a remote name", name)
	}
	return nil
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
func requestedBranch(opts exportOptions) (string, error) {
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

func confirmExport(opts exportOptions, project gitconnect.ProjectRef, request exportRequest) (bool, error) {
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

// exportOutcome is what the platform ended up with, from either path: a
// repository created for the project, or one it already had bound to it.
type exportOutcome struct {
	connection *apiclient.ProjectGitConnection
	// created distinguishes a repository this command made from one that already
	// existed, which is the difference between "Created" and "Connected".
	created bool
	// appMissing is true when Volcano could not confirm the App can see the
	// repository. Only a create reports this; a repository resolved through the
	// installations was seen through one by definition.
	appMissing bool
	installURL *string
	// pushBranch is the local branch to push, empty when the checkout is not
	// being touched.
	pushBranch string
	// sideBranch is the remote branch to push to instead of the production
	// branch, set only when the repository already has history.
	sideBranch string
	// base is the branch a pull request from sideBranch targets.
	base    string
	routing pushRouting
}

// reportExport finishes the local half and reports the outcome. The binding
// exists by now, so nothing here may return an error that reads as "nothing
// happened".
func reportExport(
	ctx context.Context,
	opts exportOptions,
	service gitconnect.Service,
	project gitconnect.ProjectRef,
	outcome exportOutcome,
) error {
	connection := outcome.connection
	local, err := pushToRepository(ctx, opts, outcome)

	settings, settingsErr := service.DeploySettings(ctx)
	creation := output.GitCreation{
		Binding: output.GitBinding{
			Connection:  connection,
			Settings:    settings,
			SettingsErr: settingsErr,
			Project:     project.Label(),
		},
		Created:      outcome.created,
		AppInstalled: !outcome.appMissing,
		InstallURL:   outcome.installURL,
		RemoteAdded:  local.remoteAdded,
		RemoteName:   opts.remote,
		Pushed:       local.pushed,
		PushedBranch: outcome.sideBranch,
		NextCommands: nextCommands(opts, outcome, local),
		Routing: output.GitPushRouting{
			Elsewhere: outcome.routing.Elsewhere,
			Target:    outcome.routing.Target,
			Source:    outcome.routing.Source,
			Err:       outcome.routing.Err,
			Command: fmt.Sprintf("git push %s %s",
				shellQuote(opts.remote), shellQuote(outcome.pushBranch)),
		},
	}
	if outcome.sideBranch != "" {
		creation.PullRequestURL = localgit.CompareURL(connection.RepoFullName, outcome.base, outcome.sideBranch)
	}
	output.GitCreated(opts.out, creation)

	if err != nil {
		// The repository exists and the project is bound to it. Saying only that
		// the push failed would read as "nothing happened", so the name goes in
		// the error too — the same rule the platform applies to every
		// post-creation failure.
		return fmt.Errorf("%s is connected, but the local step did not finish: %w",
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

// pushToRepository adds the remote and pushes.
//
// The push is skipped when the App cannot see the repository: it would succeed
// and deploy nothing, which is the one failure in this flow that produces no
// error anywhere. The remote is still recorded, because it is what the user
// needs to push by hand once access is granted.
func pushToRepository(ctx context.Context, opts exportOptions, outcome exportOutcome) (localWork, error) {
	if outcome.pushBranch == "" {
		return localWork{}, nil
	}

	client := localgit.New(opts.deps)
	if err := client.AddRemote(ctx, opts.remote, remoteURL(opts, outcome.connection.RepoFullName)); err != nil {
		return localWork{}, err
	}
	done := localWork{remoteAdded: true}
	if outcome.appMissing {
		return done, nil
	}

	// A repository with history takes the project on a branch of its own: the
	// two histories are unrelated, so a push to the production branch would be
	// refused, and forcing it would discard what is already there.
	if outcome.sideBranch != "" {
		if err := client.PushTo(ctx, opts.out, opts.remote, outcome.pushBranch, outcome.sideBranch); err != nil {
			return done, err
		}
		done.pushed = true
		return done, nil
	}
	if err := client.Push(ctx, opts.out, opts.remote, outcome.pushBranch); err != nil {
		return done, err
	}
	done.pushed = true
	return done, nil
}

func remoteURL(opts exportOptions, fullName string) string {
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
func nextCommands(opts exportOptions, outcome exportOutcome, done localWork) []string {
	remote := shellQuote(opts.remote)
	commands := make([]string, 0, 2)
	if !done.remoteAdded {
		commands = append(commands, fmt.Sprintf("git remote add %s %s",
			remote, shellQuote(remoteURL(opts, outcome.connection.RepoFullName))))
	}
	if outcome.sideBranch != "" {
		return append(commands, fmt.Sprintf("git push %s %s:%s",
			remote, shellQuote(outcome.pushBranch), shellQuote(outcome.sideBranch)))
	}
	return append(commands, fmt.Sprintf("git push --set-upstream %s %s",
		remote, shellQuote(outcome.connection.ProductionBranch)))
}

// shellQuote renders value as one POSIX shell word.
//
// These commands are printed for the user to copy into a shell, and git's own
// rules allow a branch or remote name to hold "$", "(", ")", ";", "&", "|" and
// backticks — "topic$(id)" is a branch git creates without complaint. Printed
// bare, such a name turns a command that claims to push into one that runs
// something else. The commands this CLI runs itself are unaffected: they are
// passed to git as separate arguments, with no shell in between.
func shellQuote(value string) string {
	if value != "" && !strings.ContainsFunc(value, shellUnsafe) {
		return value
	}
	// The one escape a single-quoted word needs: end the quoting, emit a literal
	// quote, start it again.
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// shellUnsafe reports whether r has to be quoted to survive a shell unchanged.
// The allowed set is deliberately small: everything a repository URL, a branch
// name or a remote name ordinarily contains, and nothing a shell reads specially.
func shellUnsafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		// "=" is absent deliberately, alongside "~": zsh expands a word starting
		// with either, and git accepts both in a ref name, so "=foo" printed bare
		// is a command zsh resolves to a path instead of pushing a branch.
		return !strings.ContainsRune("-_./:@+,", r)
	}
}
