// Package deployments wires the volcano deployments commands, which list, show
// and summarize the project's function and frontend deployments.
package deployments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/deployment"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const defaultLimit = 20

// filterFlags are the filters list and summary share.
type filterFlags struct {
	filter api.DeploymentFilter
	since  string
	after  string
}

func (f *filterFlags) bind(cmd *cobra.Command, typeUsage string) {
	flags := cmd.Flags()
	flags.StringVar(&f.filter.Type, "type", f.filter.Type, typeUsage)
	flags.StringVar(&f.filter.Kind, "kind", "", "Only standard or durable functions")
	flags.StringVar(&f.filter.Search, "name", "", "Only resources whose name contains this")
	flags.StringVar(&f.since, "since", "", "Only deployments newer than this, such as 2h or 7d")
	flags.StringVar(&f.after, "after", "", "Only deployments created at or after this RFC 3339 time")
}

func (f *filterFlags) parse(now time.Time, requireType bool) error {
	f.filter.Type = strings.ToLower(strings.TrimSpace(f.filter.Type))
	f.filter.Kind = strings.ToLower(strings.TrimSpace(f.filter.Kind))
	f.filter.Search = strings.TrimSpace(f.filter.Search)
	// A durable function is a function; naming the kind says which type.
	if f.filter.Kind != "" && f.filter.Type == "" {
		f.filter.Type = "function"
	}
	if err := deployment.Validate(f.filter, requireType); err != nil {
		return err
	}
	switch {
	case f.since != "" && f.after != "":
		return errors.New("pass --since or --after, not both")
	case f.since != "":
		since, err := parseSince(f.since)
		if err != nil {
			return err
		}
		after := now.Add(-since)
		f.filter.CreatedAfter = &after
	case f.after != "":
		after, err := time.Parse(time.RFC3339, strings.TrimSpace(f.after))
		if err != nil {
			return errors.New("--after must be an RFC 3339 time, such as 2026-10-30T09:00:00Z")
		}
		f.filter.CreatedAfter = &after
	}
	return nil
}

// parseSince reads a Go duration, plus whole days such as 7d.
func parseSince(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(value, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	since, err := time.ParseDuration(value)
	if err != nil || since <= 0 {
		return 0, fmt.Errorf("--since must be a positive duration, such as 2h or 7d, not %q", value)
	}
	return since, nil
}

// New returns the deployments command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deployments",
		Short: "List and inspect the project's deployments",
		Long: `List, show and summarize the deployments of the current project's functions,
durable functions and frontends, from every source: the CLI, a Git push, the
dashboard or the API.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newList(deps), newGet(deps), newSummary(deps))
	return cmd
}

type listOptions struct {
	deps       cliruntime.Deps
	filters    filterFlags
	limit      int
	jsonOutput bool
	out        io.Writer
	errOut     io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's deployments, newest first",
		Example: "  " + cliruntime.CommandPath(deps, "deployments list --since 1d") + "\n  " +
			cliruntime.CommandPath(deps, "deployments list --kind durable --name pipeline --json"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out, opts.errOut = cmd.OutOrStdout(), cmd.ErrOrStderr()
			return runList(cmd.Context(), opts)
		},
	}
	opts.filters.bind(cmd, "Only function or frontend deployments (default: both)")
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", defaultLimit, "Maximum deployments to list")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	if opts.limit < 1 {
		return errors.New("--limit must be at least 1")
	}
	if err := opts.filters.parse(time.Now(), false); err != nil {
		return err
	}
	deployments, more, err := deployment.NewService(opts.deps).List(ctx, opts.filters.filter, opts.limit)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		if deployments == nil {
			deployments = []apiclient.ProjectDeployment{}
		}
		if more {
			// stderr, so the JSON stays parseable.
			fmt.Fprintf(opts.errOut, "More deployments match than the %d listed. Raise --limit or narrow the filters.\n", len(deployments))
		}
		return output.JSON(opts.out, deployments)
	}
	output.Deployments(opts.out, deployments, more)
	return nil
}

type getOptions struct {
	deps       cliruntime.Deps
	id         string
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "get <deployment-id>",
		Short: "Show one deployment's status, phases, trigger and duration",
		Long: `Show one deployment: the resource it deployed, its status and trigger, when it
started and finished, each build phase, and, for a function or frontend deploy,
the command that prints its build logs.

The ID can belong to any function, durable function or frontend in the project.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.id, opts.out = args[0], cmd.OutOrStdout()
			return runGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	found, err := deployment.NewService(opts.deps).Find(ctx, opts.id)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, found)
	}
	output.Deployment(opts.out, found, buildLogsCommand(opts.deps, found))
	return nil
}

// buildLogsCommand names the command that prints a deployment's build logs.
// Durable functions are left out until their logs command takes a deployment
// ID; naming the current one could show another deploy's output. A delete
// builds nothing, and its resource is gone.
func buildLogsCommand(deps cliruntime.Deps, found *apiclient.ProjectDeployment) string {
	resource := found.Resource
	name := cliruntime.ShellQuote(resource.Name)
	switch {
	case found.Operation == apiclient.ProjectDeploymentOperationDelete:
		return ""
	case resource.Type == apiclient.ProjectDeploymentResourceTypeFrontend:
		return cliruntime.CommandPath(deps, "frontends logs "+name+" "+found.Id.String()+" --type build")
	case resource.Kind != nil && *resource.Kind == apiclient.Durable:
		return ""
	default:
		return cliruntime.CommandPath(deps, "functions logs "+name+" "+found.Id.String()+" --type build")
	}
}

type summaryOptions struct {
	deps       cliruntime.Deps
	filters    filterFlags
	jsonOutput bool
	out        io.Writer
}

func newSummary(deps cliruntime.Deps) *cobra.Command {
	opts := summaryOptions{deps: deps, filters: filterFlags{filter: api.DeploymentFilter{Type: "function"}}}
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Count the outcomes of one resource type's deployments",
		Long: `Count the deployments of the project's functions or frontends: how many there
were, succeeded, failed and were superseded, the success rate, and the median
build time. Superseded deployments count toward neither the success rate nor
the median.`,
		Example: "  " + cliruntime.CommandPath(deps, "deployments summary --since 7d") + "\n  " +
			cliruntime.CommandPath(deps, "deployments summary --type frontend --json"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runSummary(cmd.Context(), opts)
		},
	}
	opts.filters.bind(cmd, "Resource type to summarize: function or frontend")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runSummary(ctx context.Context, opts summaryOptions) error {
	if err := opts.filters.parse(time.Now(), true); err != nil {
		return err
	}
	summary, err := deployment.NewService(opts.deps).Summary(ctx, opts.filters.filter)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, summary)
	}
	output.DeploymentSummary(opts.out, summary)
	return nil
}
