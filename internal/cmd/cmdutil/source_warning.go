package cmdutil

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/deploycommit"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// sourceCheckTimeout bounds the settings read and git calls behind the
// warning, so a slow API or checkout delays a finished deploy only briefly.
const sourceCheckTimeout = 5 * time.Second

// maxNamed is how many paths or resources the warning names before it
// counts the rest.
const maxNamed = 3

// WarnUnreadableSource warns on errOut, after a cloud deploy, when LavaFlow
// is on for the project and can't read the code the deploy uploaded: why, and
// what to do instead. It reads the project's LavaFlow settings once. Local
// mode, LavaFlow off, no connected repository, or any failure prints nothing:
// the warning never changes the deploy's outcome.
func WarnUnreadableSource(ctx context.Context, deps cliruntime.Deps, errOut io.Writer, uploads *deploycommit.Uploads) {
	if deps.LocalMode || uploads.Len() == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, sourceCheckTimeout)
	defer cancel()
	settings, err := clilavaflow.NewService(deps).Settings(ctx)
	if err != nil || !settings.Enabled || settings.Repository == nil {
		return
	}
	target := deploycommit.Target{
		Repository: settings.Repository.RepoFullName,
		Branch:     settings.Repository.ProductionBranch,
	}
	unreadable := uploads.Unreadable(ctx, deps, target)
	if unreadable == nil {
		return
	}
	output.Warning(errOut, "%s", unreadableSourceMessage(unreadable, target))
}

func unreadableSourceMessage(u *deploycommit.Unreadable, target deploycommit.Target) string {
	var subject string
	switch {
	case u.Uploads == 1:
		subject = "this deployment's code to fix errors it serves"
	case len(u.Resources) == u.Uploads:
		subject = "these deployments' code to fix errors they serve"
	case len(u.Resources) == 1:
		subject = "the code deployed for " + u.Resources[0] + " to fix errors it serves"
	default:
		subject = "the code deployed for " + some(u.Resources) + " to fix errors they serve"
	}
	reasons := make([]string, 0, len(u.Reasons))
	advice := "Deploy from "
	for _, reason := range u.Reasons {
		reasons = append(reasons, reasonText(reason, target))
		if reason.Kind == deploycommit.ReasonOptedOut {
			advice = "Unset " + deploycommit.OptOutEnv + ", then deploy from "
		}
	}
	return fmt.Sprintf("LavaFlow can't read %s: %s.\n%sa clean checkout of a commit pushed to %s's %s branch, "+
		"or deploy with a Git push.", subject, strings.Join(reasons, "; "), advice, target.Repository, target.Branch)
}

func reasonText(r deploycommit.Reason, target deploycommit.Target) string {
	commit := r.SHA
	if len(commit) > 7 {
		commit = commit[:7]
	}
	switch r.Kind {
	case deploycommit.ReasonOptedOut:
		return deploycommit.OptOutEnv + "=off kept the deploy from sending its commit"
	case deploycommit.ReasonNoGit:
		return "git isn't installed, so the deploy sent no commit"
	case deploycommit.ReasonNotCheckout:
		return "the deploy didn't run in a Git checkout, so it sent no commit"
	case deploycommit.ReasonNoCommit:
		return "the checkout has no commit yet"
	case deploycommit.ReasonNotGitHub:
		return "the checkout's origin remote isn't a GitHub repository, so the deploy sent no commit"
	case deploycommit.ReasonUncommitted:
		return "the checkout has uncommitted changes (" + some(r.Paths) + ")"
	case deploycommit.ReasonDiffers:
		return "the uploaded files differ from commit " + commit
	case deploycommit.ReasonOtherRepository:
		return fmt.Sprintf("commit %s is from %s, not the project's repository %s", commit, r.Repository, target.Repository)
	case deploycommit.ReasonOffBranch:
		return fmt.Sprintf("commit %s isn't on origin/%s", commit, target.Branch)
	}
	return "the CLI couldn't read the checkout's commit"
}

// some names up to maxNamed items, then counts the rest: "a, b and c", or
// "a, b, c and 2 more".
func some(items []string) string {
	if len(items) > maxNamed {
		return fmt.Sprintf("%s and %d more", strings.Join(items[:maxNamed], ", "), len(items)-maxNamed)
	}
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}
