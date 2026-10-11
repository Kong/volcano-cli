package approvals

import (
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newDeny(deps cliruntime.Deps) *cobra.Command {
	return newDecideCommand(deps, apiclient.DurableApprovalStatusDenied,
		"deny <approval-id>",
		"Deny a pending durable approval",
		`Deny one pending approval. A denial is a decision, not a failure: the workflow
waiting on it resumes, and ctx.waitForApproval returns the decision with
approved set to false and your comment. What happens next is up to the
workflow.

Denying one that is already denied reports it and changes nothing. One that was
approved, expired, or cancelled cannot be denied; the command says which and
exits non-zero.

Only a person can decide: log in with 'volcano login'. A project access token
is refused.

By default this command prompts for confirmation, showing the approval's
title. Use --yes to skip the prompt.`,
		"  "+cliruntime.CommandPath(deps,
			`durable approvals deny 0b6f3c1e-8a4d-4f7e-9c2b-5d1a7e3f9b20 --comment "Customer cancelled"`))
}
