// Package notifications wires the volcano notifications subcommands.
package notifications

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the notifications command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "Manage where project alerts go",
		Long: `Configure how the current project is alerted when a function scheduler keeps
failing: by email, by a signed webhook, or both.

By default alerts are emailed to the project owner after 3 failed scheduled
runs in a row, and again when the scheduler recovers.`,
	}
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newSet(deps))
	cmd.AddCommand(newRotateWebhookSecret(deps))
	cmd.AddCommand(newTest(deps))
	return cmd
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
