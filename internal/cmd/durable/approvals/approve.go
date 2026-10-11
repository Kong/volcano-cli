package approvals

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newApprove(deps cliruntime.Deps) *cobra.Command {
	return newDecideCommand(deps, apiclient.DurableApprovalStatusApproved,
		"approve <approval-id>",
		"Approve a pending durable approval",
		`Approve one pending approval. The workflow waiting on it resumes, and
ctx.waitForApproval returns the decision with approved set to true and your
comment.

Approving one that is already approved reports it and changes nothing. One that
was denied, expired, or cancelled cannot be approved; the command says which
and exits non-zero.

Only a person can decide: log in with 'volcano login'. A project access token
is refused.

By default this command prompts for confirmation, showing the approval's
title. Use --yes to skip the prompt.`,
		fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "durable approvals approve 0b6f3c1e-8a4d-4f7e-9c2b-5d1a7e3f9b20"),
			cliruntime.CommandPath(deps, `durable approvals approve 0b6f3c1e-8a4d-4f7e-9c2b-5d1a7e3f9b20 --comment "Checked stock" --yes`)))
}
