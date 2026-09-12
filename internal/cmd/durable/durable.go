// Package durable wires the volcano durable subcommands.
package durable

import (
	"errors"

	"github.com/spf13/cobra"

	executionscmd "github.com/Kong/volcano-cli/internal/cmd/durable/executions"
	schedulerscmd "github.com/Kong/volcano-cli/internal/cmd/durable/schedulers"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the durable functions command. It belongs to the cloud tree
// only; NewCloudOnly stands in for it locally.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "durable",
		Short: "Manage durable functions",
		Long: `Durable functions checkpoint their progress and resume where they left off,
so one execution can run far longer than a single invocation is allowed.

They are a separate collection from standard functions: a durable function is
started rather than invoked, its executions are tracked resources, and the two
collections never accept each other's names or ids.`,
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newStart(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(executionscmd.New(deps))
	cmd.AddCommand(schedulerscmd.New(deps))
	return cmd
}

// NewCloudOnly stands in for the durable command in the local tree. Durable
// execution is checkpoint-and-replay run by the platform's regional engine, and
// the local server refuses to create a durable function rather than pretending
// to run one, so there is nothing to point the command at.
//
// Without the stub, cobra answers an unknown subcommand by printing the root's
// help and exiting 0, which reads as if the command had run. Hidden so local
// help still lists only what local mode can do, and flag parsing disabled so
// `durable deploy --all` reaches it rather than failing on an unknown flag.
func NewCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:                "durable",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New(`"durable" is a cloud command: local development does not run durable ` +
				`executions, so run 'volcano cloud durable ...' against a cloud project`)
		},
	}
}
