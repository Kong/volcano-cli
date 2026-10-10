package approvals

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	approvalID uuid.UUID
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{}
	cmd := &cobra.Command{
		Use:   "get <approval-id>",
		Short: "Get one durable approval",
		Long: `Show one approval: its title, description, and the details the workflow
attached, the workflow and execution that asked, and the decision once a person
has made one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			approvalID, err := parseApprovalID(args[0])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.approvalID = approvalID
			opts.out = cmd.OutOrStdout()
			return runGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	approval, err := clidurable.NewService(opts.deps).GetApproval(ctx, opts.approvalID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, approval)
	}
	output.DurableApproval(opts.out, approval)
	return nil
}
