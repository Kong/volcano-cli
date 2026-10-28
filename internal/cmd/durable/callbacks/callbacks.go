// Package callbacks wires the volcano durable callbacks subcommands.
package callbacks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// maxTokenInput bounds what is read from stdin for a token, which is far
// shorter.
const maxTokenInput = 4096

// New returns the durable callbacks command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "callbacks",
		Short: "Answer durable callbacks with their token",
		Long: `A callback is a wait a durable workflow hands to another system. The workflow
calls ctx.waitForCallback, passes the token it gets to that system, and suspends
until the token is succeeded or failed, or its timeout passes.

These commands are that system's side. The token is the only credential they
send, so they need no login or project: whoever holds the token can answer the
wait. Treat it as a secret. Pass - in place of the token to read it from stdin,
which keeps it out of your shell history and the process list.

A token takes one answer. Once it has succeeded or failed, every later answer
or heartbeat is refused, including a retry of the answer that went through.`,
	}
	// Answering needs no project, so a project link that can't be used must not
	// stop it.
	if deps.ConfigLoader == nil {
		deps.ConfigLoader = config.LoadWithoutLink
	}
	cmd.AddCommand(newSucceed(deps))
	cmd.AddCommand(newFail(deps))
	cmd.AddCommand(newHeartbeat(deps))
	return cmd
}

// readToken returns the token argument, or the token on stdin when the
// argument is "-".
func readToken(arg string, in io.Reader) (string, error) {
	if arg != "-" {
		return strings.TrimSpace(arg), nil
	}
	data, err := io.ReadAll(io.LimitReader(in, maxTokenInput))
	if err != nil {
		return "", fmt.Errorf("failed to read the callback token from stdin: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("no callback token on stdin")
	}
	return token, nil
}

// reportAnswer prints what became of a succeed or fail. delivering is not a
// failure: the outcome is recorded, and sending it again would be refused.
func reportAnswer(w io.Writer, outcome *apiclient.DurableCallbackOutcome, done string) {
	if outcome.Status == apiclient.DurableCallbackStatusDelivering {
		output.Success(w, "Volcano recorded the answer and is still delivering it to the execution; do not send it again")
		return
	}
	output.Success(w, "%s", done)
}

// writeJSON prints value as indented JSON for --json consumers.
func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
