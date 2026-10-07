// Package approvals wires the volcano durable approvals subcommands.
package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

// New returns the durable approvals command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "List and decide durable approvals",
		Long: `An approval is a decision a durable workflow waits on. The workflow asks for
one with ctx.waitForApproval and suspends until a person approves or denies it,
or until its timeout passes. Either way the workflow resumes with the outcome.

Anyone with access to the project can list approvals and read their stats.
Only a person can decide one: approve and deny refuse a project access token.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newStats(deps))
	cmd.AddCommand(newApprove(deps))
	cmd.AddCommand(newDeny(deps))
	return cmd
}

type decideOptions struct {
	deps       cliruntime.Deps
	approvalID uuid.UUID
	decision   apiclient.DurableApprovalStatus
	comment    string
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	prompt     io.Writer
}

func newDecideCommand(
	deps cliruntime.Deps, decision apiclient.DurableApprovalStatus, use, short, long, example string,
) *cobra.Command {
	opts := decideOptions{decision: decision}
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			approvalID, err := parseApprovalID(args[0])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.approvalID = approvalID
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			// The prompt stays off stdout under --json, so what a script reads
			// there is only the approval.
			opts.prompt = opts.out
			if opts.jsonOutput {
				opts.prompt = cmd.ErrOrStderr()
			}
			return runDecide(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.comment, "comment", "", "Note for the workflow and the approval's history")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

// runDecide reads the approval before deciding, both to show the person what
// they are deciding and to answer without a write when nothing is left to
// decide.
func runDecide(ctx context.Context, opts decideOptions) error {
	service := clidurable.NewService(opts.deps)
	approval, err := service.GetApproval(ctx, opts.approvalID)
	if err != nil {
		return err
	}
	if conflict := clidurable.ApprovalConflict(approval, opts.decision); conflict != nil {
		return conflict
	}

	verb := "Approve"
	if opts.decision == apiclient.DurableApprovalStatusDenied {
		verb = "Deny"
	}

	if approval.Status == opts.decision {
		if opts.jsonOutput {
			return writeJSON(opts.out, approval)
		}
		output.DurableApproval(opts.out, approval)
		output.Note(opts.out, "Already %s%s; nothing changed", approval.Status,
			clidurable.DecisionSummary(approval.Decision))
		return nil
	}

	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.prompt,
			fmt.Sprintf("%q was requested by workflow %s. The workflow resumes with your decision, "+
				"and a decision cannot be changed.",
				theme.StripControl(approval.Title), theme.StripControl(approval.Function.Name)),
			verb+" it?")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	decided, err := service.DecideApproval(ctx, opts.approvalID, opts.decision, opts.comment)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, decided)
	}
	output.DurableApproval(opts.out, decided)
	output.Success(opts.out, "%s %q; the workflow resumes with the decision",
		pastTense(opts.decision), theme.StripControl(decided.Title))
	return nil
}

func pastTense(decision apiclient.DurableApprovalStatus) string {
	if decision == apiclient.DurableApprovalStatusDenied {
		return "Denied"
	}
	return "Approved"
}

// parseApprovalID refuses a malformed id here, where the message can name the
// argument, rather than as a validation error from the API.
func parseApprovalID(value string) (uuid.UUID, error) {
	approvalID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid approval ID %q: %w", value, err)
	}
	return approvalID, nil
}

// parseSince reads a lookback window. It takes Go durations such as 24h or
// 90m, plus whole days such as 30d, because days are the unit approval
// history is usually read in and time.ParseDuration has none.
func parseSince(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	var window time.Duration
	if days, found := strings.CutSuffix(trimmed, "d"); found {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, fmt.Errorf("invalid --since %q: use a duration such as 30d, 24h, or 90m", value)
		}
		window = time.Duration(n) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(trimmed)
		if err != nil {
			return 0, fmt.Errorf("invalid --since %q: use a duration such as 30d, 24h, or 90m", value)
		}
		window = parsed
	}
	if window <= 0 {
		return 0, fmt.Errorf("invalid --since %q: the window has to be longer than zero", value)
	}
	return window, nil
}

// sinceTime converts a --since value to the start of the window it names.
// Sent in UTC so the query string carries no offset to encode.
func sinceTime(value string, now time.Time) (*time.Time, error) {
	window, err := parseSince(value)
	if err != nil {
		return nil, err
	}
	from := now.Add(-window).UTC()
	return &from, nil
}

// writeJSON prints value as indented JSON for --json consumers.
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
