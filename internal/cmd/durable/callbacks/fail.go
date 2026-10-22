package callbacks

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type failOptions struct {
	deps       cliruntime.Deps
	token      string
	errorType  string
	message    string
	jsonOutput bool
	out        io.Writer
}

func newFail(deps cliruntime.Deps) *cobra.Command {
	opts := failOptions{}
	cmd := &cobra.Command{
		Use:   "fail <token>",
		Short: "Resume a waiting step with an error",
		Long: `Fail one callback. The step waiting on the token resumes by raising
CallbackFailedError, carrying --type and --message. The workflow can catch it
and branch on the type; left uncaught, it fails the execution.

--type is required: a name the workflow can branch on, such as CardDeclined, up
to 256 characters without control characters. --message is a human-readable
explanation, up to 32 KiB.

The command returns once the execution has the error, or says Volcano is still
delivering it, as succeed does.

--json prints the outcome as the API returns it.`,
		Example: "  " + cliruntime.CommandPath(deps,
			`durable callbacks fail vcb_0000000000000000000000000000000000000000000 --type CardDeclined --message "Insufficient funds"`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.errorType) == "" {
				return errors.New("--type needs a name the workflow can branch on")
			}
			token, err := readToken(args[0], cmd.InOrStdin())
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.token = token
			opts.out = cmd.OutOrStdout()
			return runFail(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.errorType, "type", "", "Error type the workflow branches on")
	cmd.Flags().StringVar(&opts.message, "message", "", "Human-readable explanation of the error")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func runFail(ctx context.Context, opts failOptions) error {
	outcome, err := clidurable.NewService(opts.deps).FailCallback(ctx, opts.token, opts.errorType, opts.message)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, outcome)
	}
	reportAnswer(opts.out, outcome, "Failed the callback; the execution resumes with the error")
	return nil
}
