package git

import (
	"context"
	"errors"
	"fmt"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/localgit"
)

// sideBranchName is where the project goes when the repository already has a
// history of its own.
//
// A fixed name rather than one per run, so exporting again updates the same
// branch and the same pull request instead of leaving a trail of them.
const sideBranchName = "volcano/export"

// resolveTarget decides which of the three shapes an export takes, by asking
// whether the named repository exists and whether it has any history.
//
// The caller states the repository, not the shape. Which of the three applies is
// a fact about GitHub, and asking the user to declare it would mean asking them
// to know something they cannot check from a terminal.
type resolveTarget struct {
	// existing is the repository the App can already see, nil when there is none
	// to bind and one has to be created.
	existing *gitconnect.Target
	// sideBranch is the remote branch to push to instead of the production
	// branch, set only when the repository already has history.
	sideBranch string
}

// resolveExportTarget looks the repository up among the caller's installations.
//
// Not found is taken as "does not exist", which is the ordinary reading, and the
// other reading — it exists but the App cannot see it — is answered later and
// better: creating it returns GitHub's own "name already taken", which says both
// that the repository is there and that this is not the way to reach it.
func resolveExportTarget(
	ctx context.Context, opts exportOptions, service gitconnect.Service, plan checkoutPlan, owner, name string,
) (resolveTarget, error) {
	if owner == "" {
		// Without an owner there is nothing to look up: which account "your own"
		// means is the platform's to resolve, and guessing a login here would
		// look the wrong repository up and create under the wrong account.
		return resolveTarget{}, nil
	}

	target, err := service.Resolve(ctx, localgit.Repository{Owner: owner, Name: name})
	if err != nil {
		if errors.Is(err, gitconnect.ErrRepositoryNotAccessible) {
			return resolveTarget{}, nil
		}
		return resolveTarget{}, err
	}

	sideBranch, err := sideBranchFor(ctx, opts, plan, target.Repository.FullName)
	if err != nil {
		return resolveTarget{}, err
	}
	return resolveTarget{existing: target, sideBranch: sideBranch}, nil
}

// sideBranchFor asks the repository whether it has any history, which is what
// separates an empty repository from one the project cannot simply be pushed
// into.
//
// Read before anything is bound, because it decides what the user is asked to
// confirm. Nothing is read when there is no push to route: --no-push touches
// neither the checkout nor the network.
func sideBranchFor(
	ctx context.Context, opts exportOptions, plan checkoutPlan, fullName string,
) (string, error) {
	if plan.branch == "" {
		return "", nil
	}

	hasHistory, err := localgit.New(opts.deps).HasRemoteBranches(ctx, remoteURL(opts, fullName))
	if err != nil {
		return "", fmt.Errorf(
			"%w\n\nThis decides whether your history can be pushed as it is, so it is not guessed at", err)
	}
	if !hasHistory {
		return "", nil
	}
	return sideBranchName, nil
}

// bindExisting binds the project to a repository that already exists.
//
// An empty repository is then pointed at the branch about to be pushed: it has
// no real default branch, only the account's configured name, so what the bind
// recorded is a prediction. A repository with history keeps its own default
// branch as the production branch — that is where the pull request lands, and
// therefore what should deploy.
func bindExisting(
	ctx context.Context,
	service gitconnect.Service,
	target resolveTarget,
	root string,
	plan checkoutPlan,
) (exportOutcome, error) {
	// nil expected: this project had no binding when it was read, and the bind is
	// a full replace that names no prior state. Passing it makes the write refuse
	// rather than silently discard a binding made while the prompt was open.
	connection, err := service.Connect(ctx, *target.existing, root, nil)
	if err != nil {
		return exportOutcome{}, err
	}

	if target.sideBranch == "" && plan.branch != "" && connection.ProductionBranch != plan.branch {
		if connection, err = service.SetProductionBranch(ctx, plan.branch); err != nil {
			return exportOutcome{}, fmt.Errorf("%s is connected, but its production branch is still %s: %w",
				target.existing.Repository.FullName, connection.ProductionBranch, err)
		}
	}

	return exportOutcome{
		connection: connection,
		pushBranch: plan.branch,
		sideBranch: target.sideBranch,
		base:       connection.ProductionBranch,
		routing:    plan.routing,
	}, nil
}

// existingRepoError explains the failure a caller can act on — the App cannot see
// the repository they named — and hands everything else back unchanged.
func existingRepoError(webURL, owner string, err error) error {
	if !errors.Is(err, gitconnect.ErrRepositoryNotAccessible) {
		return err
	}
	return fmt.Errorf("%w\n\nEither the Volcano GitHub App is not installed on %s, or it is installed "+
		"for selected repositories that do not include this one.\n\n%s", err, owner,
		dashboardStep(webURL, "Grant it access, then run this command again:"))
}
