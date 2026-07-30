// Package databases wires the volcano databases subcommands.
package databases

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps                 cliruntime.Deps
	name                 string
	region               string
	postgresVersion      string
	databaseType         string
	showConnectionString bool
	defaults             LocalCreateDefaultsFunc
	out                  io.Writer
}

type createCommandOptions struct {
	short                  string
	long                   string
	requireRegion          bool
	requirePostgresVersion bool
	defaults               LocalCreateDefaultsFunc
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	return newCreateWithOptions(deps, createCommandOptions{
		short: "Create a cloud database",
		long: fmt.Sprintf(`Create a PostgreSQL database in the current cloud project.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases create app --region <region> --pg-version <version>"),
			cliruntime.CommandPath(deps, "databases create analytics --region <region> --pg-version <version> --type volcano-db-s")),
		requireRegion:          true,
		requirePostgresVersion: true,
	})
}

func newLocalCreate(deps cliruntime.Deps, defaults LocalCreateDefaultsFunc) *cobra.Command {
	return newCreateWithOptions(deps, createCommandOptions{
		short:    "Create a local database",
		long:     "Create a PostgreSQL database in the local development project.",
		defaults: defaults,
	})
}

func newCreateWithOptions(deps cliruntime.Deps, commandOpts createCommandOptions) *cobra.Command {
	var region string
	var pgVersion string
	var databaseType string
	var showConnectionString bool
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: commandOpts.short,
		Long:  commandOpts.long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), createOptions{
				deps:                 deps,
				name:                 strings.TrimSpace(args[0]),
				region:               region,
				postgresVersion:      pgVersion,
				databaseType:         databaseType,
				showConnectionString: showConnectionString,
				defaults:             commandOpts.defaults,
				out:                  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&region, "region", "", "Database region (aws-<aws-region>, e.g. aws-us-east-1)")
	cmd.Flags().StringVar(&pgVersion, "pg-version", "", "PostgreSQL version")
	cmd.Flags().StringVar(&databaseType, "type", "", "Database type (default: volcano-db-xs)")
	cmd.Flags().BoolVar(&showConnectionString, "show-connection-string", false, "Show database connection string")
	if commandOpts.requireRegion {
		_ = cmd.MarkFlagRequired("region")
	}
	if commandOpts.requirePostgresVersion {
		_ = cmd.MarkFlagRequired("pg-version")
	}
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	opts.region = strings.TrimSpace(opts.region)
	opts.postgresVersion = strings.TrimSpace(opts.postgresVersion)

	if opts.defaults != nil && (opts.region == "" || opts.postgresVersion == "") {
		defaults, err := opts.defaults(ctx)
		if err != nil {
			return err
		}
		if opts.region == "" {
			opts.region = strings.TrimSpace(defaults.Region)
		}
		if opts.postgresVersion == "" {
			opts.postgresVersion = strings.TrimSpace(defaults.PostgresVersion)
		}
	}
	if opts.region == "" {
		return errors.New("database region is required")
	}
	if opts.postgresVersion == "" {
		return errors.New("postgres version is required")
	}

	database, err := clidatabase.NewService(opts.deps).Create(ctx, opts.name, opts.region, opts.postgresVersion, opts.databaseType)
	if err != nil {
		return err
	}

	output.DatabaseCreated(opts.out, database, opts.showConnectionString)
	return nil
}
