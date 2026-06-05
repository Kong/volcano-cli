package storage

import (
	"github.com/spf13/cobra"

	bucketcmd "github.com/Kong/volcano-cli/internal/cmd/storage/bucket"
	objectcmd "github.com/Kong/volcano-cli/internal/cmd/storage/object"
	policycmd "github.com/Kong/volcano-cli/internal/cmd/storage/policy"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type commandOptions struct {
	storageOptions []clistorage.Option
}

// Option configures storage command behavior.
type Option func(*commandOptions)

// WithObjectTokenProvider configures the bearer token source for storage object routes.
func WithObjectTokenProvider(provider clistorage.ObjectTokenProvider) Option {
	return func(opts *commandOptions) {
		opts.storageOptions = append(opts.storageOptions, clistorage.WithObjectTokenProvider(provider))
	}
}

// New returns the storage command.
func New(deps cliruntime.Deps) *cobra.Command {
	return NewWithOptions(deps)
}

// NewWithOptions returns the storage command with custom storage behavior.
func NewWithOptions(deps cliruntime.Deps, options ...Option) *cobra.Command {
	var opts commandOptions
	for _, option := range options {
		option(&opts)
	}

	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Manage storage",
		Long:  "Manage storage buckets, policies, and objects in the current project.",
	}
	cmd.AddCommand(bucketcmd.New(deps))
	cmd.AddCommand(policycmd.New(deps))
	cmd.AddCommand(objectcmd.NewWithServiceOptions(deps, opts.storageOptions...))
	cmd.AddCommand(newStats(deps))
	return cmd
}
