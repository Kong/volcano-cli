package databases

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type catalogOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newRegions(deps cliruntime.Deps) *cobra.Command {
	opts := catalogOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "regions",
		Short: "List the regions a database can be created in",
		Long: `List the regions a database can be created in: the values
'databases create --region' accepts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runRegions(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRegions(ctx context.Context, opts catalogOptions) error {
	regions, err := clidatabase.NewService(opts.deps).Regions(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, regions)
	}
	output.DatabaseRegions(opts.out, regions)
	return nil
}

func newVersions(deps cliruntime.Deps) *cobra.Command {
	opts := catalogOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "versions",
		Short: "List the PostgreSQL versions a database can be created on",
		Long: `List the PostgreSQL major versions a database can be created on, newest
first: the values 'databases create --pg-version' accepts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runVersions(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runVersions(ctx context.Context, opts catalogOptions) error {
	versions, err := clidatabase.NewService(opts.deps).Versions(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, versions)
	}
	output.PostgresVersions(opts.out, versions)
	return nil
}
