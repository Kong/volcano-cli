// Package messages wires the volcano durable messages subcommands.
package messages

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the durable messages command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Send and read durable execution messages",
		Long: `A message is a named JSON value exchanged with a running execution. The
workflow receives one with ctx.waitForMessage and publishes one with
ctx.publish. A message sent before the workflow waits for its name is kept
until it does, and waits take messages oldest first.

Anyone with access to the project can send, list, and see what an execution is
waiting for. A read-only project access token can list but not send.`,
	}
	cmd.AddCommand(newSend(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newWaits(deps))
	return cmd
}

// parseExecutionID refuses a name here rather than sending it to a route that
// takes an id: an execution's name is only an idempotency key.
func parseExecutionID(value string) (uuid.UUID, error) {
	executionID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid execution ID %q: %w", value, err)
	}
	return executionID, nil
}

// writeJSON prints value as indented JSON for --json consumers.
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
