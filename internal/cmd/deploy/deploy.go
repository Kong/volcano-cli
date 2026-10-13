// Package deploy wires "volcano deploy" and "volcano cloud deploy", which
// deploy everything a project directory describes in one run.
package deploy

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

type options struct {
	deps           cliruntime.Deps
	withMigrations bool
	database       string
	envFile        string
	noEnvFile      bool
	configs        []string
	noFrontends    bool
	noWait         bool
	out            io.Writer
	errOut         io.Writer
}

// New returns "volcano cloud deploy", which deploys to the selected project.
func New(deps cliruntime.Deps) *cobra.Command {
	return newCommand(deps, "Deploy the whole project to the selected cloud project")
}

// NewLocal returns "volcano deploy", which deploys to the local development
// server.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	return newCommand(deps, "Deploy the whole project to the local development server")
}

func newCommand(deps cliruntime.Deps, short string) *cobra.Command {
	opts := options{}
	databaseHelp := "Database to apply migrations to (default: the one the manifests declare)"
	deployExample := "deploy --env-file volcano/cloud.env"
	migrateExample := "deploy --env-file volcano/cloud.env --with-migrations -d app"
	manifestsExample := "deploy --env-file volcano/staging.env -c volcano/volcano-config.yaml -c volcano/auth.yaml"
	envNote := `

volcano/volcano.env holds the local server's values. Name the cloud project's
env file with --env-file; one holding the local server's keys or a localhost
VOLCANO_API_URL is refused.`
	if deps.LocalMode {
		databaseHelp = "Database to apply migrations to (default: " + defaultLocalDatabase + ")"
		deployExample = "deploy"
		migrateExample = "deploy --with-migrations"
		manifestsExample = "deploy -c volcano/volcano-config.yaml -c volcano/auth.local.yaml"
		envNote = ""
	}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: short,
		Long: fmt.Sprintf(`Deploy everything the project directory describes, in the order the platform
needs:

  1. variables   volcano/volcano.env, or the variable sections of the manifests
  2. migrations  volcano/migrations/*.sql, only with --with-migrations
  3. functions   the standard functions in volcano/functions
  4. durable     the functions the first manifest declares durable
  5. frontends   the frontends listed in .volcano/source-layout.json
  6. config      each manifest, in the order given

A step with nothing to deploy is skipped. The env file, the manifests, and the
source layout are read and checked before anything changes; function sources
and frontend directories are packaged in their own steps. The run stops at the
first step that fails. Afterwards the command waits for the deployments it
started and prints each frontend's URL; --no-wait returns as soon as the config
step is done.

Variables come from the env file or from a manifest's variables section, never
both: applying that section deletes every variable it does not list. Pass
--no-env-file when a manifest holds them.%s

Migrations run without tracking, so every file runs again on each
--with-migrations run. Write them to be safe to re-run.

Usage:
  %s
  %s
  %s`,
			envNote,
			cliruntime.CommandPath(deps, deployExample),
			cliruntime.CommandPath(deps, migrateExample),
			cliruntime.CommandPath(deps, manifestsExample)),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			run := opts
			run.deps = deps
			run.database = strings.TrimSpace(run.database)
			run.envFile = strings.TrimSpace(run.envFile)
			run.out = cmd.OutOrStdout()
			run.errOut = cmd.ErrOrStderr()
			return runDeploy(cmd.Context(), run)
		},
	}
	cmd.Flags().BoolVar(&opts.withMigrations, "with-migrations", false, "Apply every migration in volcano/migrations")
	cmd.Flags().StringVarP(&opts.database, "database", "d", "", databaseHelp)
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Env file to deploy (default: volcano/volcano.env or ./volcano.env)")
	cmd.Flags().BoolVar(&opts.noEnvFile, "no-env-file", false, "Deploy no env file, even when volcano/volcano.env exists")
	cmd.Flags().StringArrayVarP(&opts.configs, "config", "c", nil, "Manifest to apply; repeat to apply several in order (default: volcano/volcano-config.yaml or ./volcano-config.yaml)")
	cmd.Flags().BoolVar(&opts.noFrontends, "no-frontends", false, "Skip frontends")
	cmd.Flags().BoolVar(&opts.noWait, "no-wait", false, "Return without waiting for deployments to finish")
	return cmd
}

func runDeploy(ctx context.Context, opts options) error {
	start := now(opts.deps)
	p, err := prepare(ctx, opts)
	if err != nil {
		return err
	}
	p.print(opts.out, opts.errOut)

	r := &runner{opts: opts}
	on := theme.On(opts.out)
	var done []string
	for i, s := range p.steps {
		if s.skipped {
			continue
		}
		fmt.Fprintf(opts.out, "\n%s\n", theme.Title(fmt.Sprintf("[%d/%d] %s", i+1, len(p.steps), s.name), on))
		if err := s.run(ctx, r); err != nil {
			return fmt.Errorf("%s: %w\n\n%s", s.name, err, resumeHint(done, p.steps[i:]))
		}
		done = append(done, s.name)
	}

	if opts.noWait {
		fmt.Fprintln(opts.out)
		output.Success(opts.out, "Deployments started; not waiting for them to finish")
		return nil
	}
	if err := r.wait(ctx, start); err != nil {
		return err
	}
	fmt.Fprintln(opts.out)
	output.Success(opts.out, "Deployed in %s", now(opts.deps).Sub(start).Round(time.Second))
	return nil
}

// resumeHint says what already ran and how to finish from the failed step.
func resumeHint(done []string, rest []step) string {
	var b strings.Builder
	if len(done) > 0 {
		fmt.Fprintf(&b, "Already deployed: %s.\n", strings.Join(done, ", "))
	}
	b.WriteString("Fix the cause and run the command again")
	if slices.Contains(done, stepMigrations) {
		b.WriteString(" without --with-migrations and -d, since the migrations already ran")
	}
	var manual []string
	for _, s := range rest {
		if !s.skipped {
			manual = append(manual, s.manual...)
		}
	}
	if len(manual) == 0 {
		b.WriteString(".")
		return b.String()
	}
	b.WriteString(", or finish by hand:")
	for _, command := range manual {
		b.WriteString("\n  " + command)
	}
	return b.String()
}

func now(deps cliruntime.Deps) time.Time {
	if deps.Now != nil {
		return deps.Now()
	}
	return time.Now()
}
