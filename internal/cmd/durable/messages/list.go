package messages

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

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	defaultListLimit = 100
	followInterval   = 2 * time.Second
)

type listOptions struct {
	deps        cliruntime.Deps
	function    string
	executionID uuid.UUID
	direction   string
	name        string
	after       int64
	limit       int
	follow      bool
	jsonOutput  bool
	out         io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list <function> <execution-id>",
		Short: "List a durable execution's messages",
		Long: `List the messages sent to an execution (direction in) and the ones it
published (direction out), oldest first. Every message has a seq; --after reads
only the messages after one.

--follow keeps reading as messages arrive, until the execution ends. With
--json it prints one message per line.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "durable messages list order-pipeline 6f1c2a9e-3b7d-4c8a-9e2f-1a5b7c9d0e3f"),
			cliruntime.CommandPath(deps, "durable messages list order-pipeline 6f1c2a9e-3b7d-4c8a-9e2f-1a5b7c9d0e3f --direction out --follow")),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.executionID = executionID
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.direction, "direction", "", "Only messages travelling this way (in, out)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Only messages with this name")
	cmd.Flags().Int64Var(&opts.after, "after", 0, "Only messages with a seq greater than this")
	cmd.Flags().IntVar(&opts.limit, "limit", defaultListLimit, "Most messages to read per request")
	cmd.Flags().BoolVarP(&opts.follow, "follow", "f", false, "Keep reading messages until the execution ends")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	direction := strings.ToLower(strings.TrimSpace(opts.direction))
	if direction != "" && !apiclient.DurableMessageDirection(direction).Valid() {
		return fmt.Errorf("unknown message direction %q: --direction takes in or out", opts.direction)
	}
	if opts.after < 0 {
		return fmt.Errorf("invalid --after %d: a seq is never negative", opts.after)
	}
	input := api.DurableMessageListInput{
		Direction: direction,
		Name:      opts.name,
		After:     opts.after,
		Limit:     opts.limit,
	}

	service := clidurable.NewService(opts.deps)
	if opts.follow {
		return runFollow(ctx, opts, service, input)
	}
	messages, err := service.ListMessages(ctx, opts.function, opts.executionID, input)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, messages)
	}
	output.DurableMessages(opts.out, messages, listCommand(opts, direction))
	return nil
}

// runFollow reads the execution's status before each pass and drains its
// messages after. A message is published while the execution runs and none is
// sent once it has ended, so the pass after it is seen to end has read
// everything there will be.
func runFollow(ctx context.Context, opts listOptions, service clidurable.Service, input api.DurableMessageListInput) error {
	ticker := cliruntime.NewTicker(opts.deps, followInterval)
	defer ticker.Stop()

	printed := 0
	for {
		execution, err := service.GetExecution(ctx, opts.function, opts.executionID)
		if interrupted(ctx) {
			return nil
		}
		if err != nil {
			return err
		}
		for {
			page, err := service.ListMessages(ctx, opts.function, opts.executionID, input)
			if interrupted(ctx) {
				return nil
			}
			if err != nil {
				return err
			}
			for i := range page.Data {
				if err := printFollowed(opts, &page.Data[i], printed); err != nil {
					return err
				}
				printed++
				input.After = page.Data[i].Seq
			}
			if !page.HasMore || len(page.Data) == 0 {
				break
			}
		}

		if clidurable.ExecutionEnded(execution.Status) {
			if !opts.jsonOutput {
				if printed == 0 {
					fmt.Fprintln(opts.out, "No messages found")
				}
				output.Note(opts.out, "Execution %s %s; it takes and publishes no more messages",
					theme.StripControl(execution.Name), execution.Status)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C():
		}
	}
}

func printFollowed(opts listOptions, message *apiclient.DurableMessage, printed int) error {
	if opts.jsonOutput {
		return json.NewEncoder(opts.out).Encode(message)
	}
	if printed == 0 {
		output.DurableMessageTableHead(opts.out)
	}
	output.DurableMessageTableRow(opts.out, message)
	return nil
}

// interrupted reports whether the follow was interrupted, which is how a
// follow of a long execution usually ends, so a read it cut short is not a
// failure.
func interrupted(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// listCommand rebuilds the command the messages were listed with, minus
// --after, so the hint for the rest keeps every filter.
func listCommand(opts listOptions, direction string) string {
	parts := []string{cliruntime.CommandPath(opts.deps,
		"durable messages list "+opts.function+" "+opts.executionID.String())}
	if direction != "" {
		parts = append(parts, "--direction", direction)
	}
	if opts.name != "" {
		parts = append(parts, "--name", shellWord(opts.name))
	}
	if opts.limit != defaultListLimit {
		parts = append(parts, "--limit", strconv.Itoa(opts.limit))
	}
	return strings.Join(parts, " ")
}

// shellWord quotes a message name for a shell when it needs it. A name is any
// printable ASCII, so it can hold spaces and quotes.
func shellWord(value string) string {
	if strings.IndexFunc(value, needsShellQuote) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(theme.StripControl(value), "'", `'\''`) + "'"
}

func needsShellQuote(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return !strings.ContainsRune("-_.:/", r)
	}
}
