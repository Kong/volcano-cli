package functions

import (
	"github.com/spf13/cobra"

	schedulerscmd "github.com/Kong/volcano-cli/internal/cmd/functions/schedulers"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Option configures function command behavior.
type Option func(*commandOptions)

// WithInvokeTokenProvider configures the bearer token source for function invoke routes.
func WithInvokeTokenProvider(provider clifunction.InvokeTokenProvider) Option {
	return func(opts *commandOptions) {
		opts.functionOptions = append(opts.functionOptions, clifunction.WithInvokeTokenProvider(provider))
	}
}

// New returns the functions command.
func New(deps cliruntime.Deps) *cobra.Command {
	return NewWithOptions(deps)
}

// NewWithOptions returns the functions command with custom function behavior.
func NewWithOptions(deps cliruntime.Deps, options ...Option) *cobra.Command {
	opts := commandOptions{batchDeployAll: true}
	for _, option := range options {
		option(&opts)
	}
	return newWithOptions(deps, opts)
}

// NewLocal returns the functions command for local-mode projects.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	return newWithOptions(deps, commandOptions{batchDeployAll: false})
}

type commandOptions struct {
	batchDeployAll  bool
	functionOptions []clifunction.Option
}

func newWithOptions(deps cliruntime.Deps, opts commandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "functions",
		Short: "Manage functions",
		Long:  "List, inspect, update, delete, and view logs for project functions.",
	}
	cmd.AddCommand(newDeploy(deps, opts.batchDeployAll))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newInvoke(deps, opts.functionOptions...))
	cmd.AddCommand(newAlias(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newUpdate(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newRuntimes(deps))
	cmd.AddCommand(schedulerscmd.New(deps))
	return cmd
}
