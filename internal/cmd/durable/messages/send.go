package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

type sendOptions struct {
	deps           cliruntime.Deps
	function       string
	executionID    uuid.UUID
	name           string
	data           json.RawMessage
	idempotencyKey string
	jsonOutput     bool
	out            io.Writer
	errOut         io.Writer
}

func newSend(deps cliruntime.Deps) *cobra.Command {
	opts := sendOptions{}
	var data, dataFile string
	cmd := &cobra.Command{
		Use:   "send <function> <execution-id> <name>",
		Short: "Send a message to a durable execution",
		Long: `Send one named message to an execution. The workflow receives it from
ctx.waitForMessage with the same name; until then the message is queued. An
execution that has ended takes no messages.

--data is the message's JSON value, and --data-file reads it from a file.
Without either the workflow receives null.

--idempotency-key makes a send safe to retry: a repeat with a key the execution
has already seen returns the first message and sends nothing. Without it, a key
is generated and printed to stderr before the message is sent; pass it to
--idempotency-key to retry a send whose outcome you did not see.

--json prints the message as the API returns it, so a repeat prints the first
message just as the send that queued it did.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps,
				`durable messages send order-pipeline 6f1c2a9e-3b7d-4c8a-9e2f-1a5b7c9d0e3f payment-confirmed --data '{"amount":4200}'`),
			cliruntime.CommandPath(deps,
				"durable messages send order-pipeline 6f1c2a9e-3b7d-4c8a-9e2f-1a5b7c9d0e3f document-posted --data-file post.json --idempotency-key post-4417")),
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
			}
			opts.data, err = messageData(cmd, data, dataFile)
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.executionID = executionID
			opts.name = args[2]
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			return runSend(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "The message's JSON value")
	cmd.Flags().StringVar(&dataFile, "data-file", "", "Path to a file holding the message's JSON value")
	cmd.Flags().StringVar(&opts.idempotencyKey, "idempotency-key", "", "Key that makes the send safe to retry")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.MarkFlagsMutuallyExclusive("data", "data-file")
	return cmd
}

// messageData reads the flag that carries the data. Neither flag is no data,
// which is not the same as data that is an empty string.
func messageData(cmd *cobra.Command, data, dataFile string) (json.RawMessage, error) {
	switch {
	case cmd.Flags().Changed("data"):
		return cmdutil.ParseJSONValue("data", []byte(data))
	case cmd.Flags().Changed("data-file"):
		if strings.TrimSpace(dataFile) == "" {
			return nil, errors.New("--data-file needs a path")
		}
		contents, err := cmdutil.ReadJSONFile("data", dataFile)
		if err != nil {
			return nil, err
		}
		return cmdutil.ParseJSONValue("data", contents)
	default:
		return nil, nil
	}
}

func runSend(ctx context.Context, opts sendOptions) error {
	if opts.idempotencyKey == "" {
		opts.idempotencyKey = uuid.NewString()
		// Printed before sending: when the response is lost, this is the only
		// way to retry without queueing the message twice.
		output.Note(opts.errOut, "Idempotency key %s; rerun with --idempotency-key %s to retry this send safely",
			opts.idempotencyKey, opts.idempotencyKey)
	}
	message, created, err := clidurable.NewService(opts.deps).SendMessage(
		ctx, opts.function, opts.executionID, opts.name, opts.data, opts.idempotencyKey)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, message)
	}
	output.DurableMessage(opts.out, message)
	if !created {
		output.Note(opts.out, "Already sent with this idempotency key; nothing changed")
		return nil
	}
	output.Success(opts.out, "Sent %q to execution %s", theme.StripControl(message.Name), opts.executionID)
	return nil
}
