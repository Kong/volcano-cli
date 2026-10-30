// Package logs wires the volcano logs commands, which search, tail and count a
// project's runtime logs across its resources.
package logs

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
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	"github.com/Kong/volcano-cli/internal/logfollow"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectlog"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	defaultLimit   = 100
	defaultBuckets = 24
	maxBuckets     = 96
)

// queryFlags are the selector and filters every logs command takes.
type queryFlags struct {
	selector projectlog.Selector
	filters  projectlog.Filters
	since    string
	start    string
	end      string
}

func (f *queryFlags) bind(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.selector.Type, "type", projectlog.TypeFunction, "Resource type to read: function, frontend or database")
	flags.StringVar(&f.selector.Kind, "kind", "", "Read only standard or durable functions (default: both)")
	flags.StringSliceVar(&f.selector.Names, "name", nil,
		"Resource name or ID to read; repeat or separate with commas for several (default: every resource of the type)")
	flags.StringVarP(&f.filters.Query, "query", "q", "", `Log query, such as 'timeout OR "connection reset"'`)
	flags.StringVar(&f.filters.Level, "level", "", "Only logs at this level: "+strings.Join(projectlog.Levels, ", "))
	flags.StringVar(&f.filters.Region, "region", "", "Only logs from this region, such as us-east-1")
	flags.StringVar(&f.filters.Invocation, "invocation", "", "Only logs from this function invocation ID")
	flags.StringVar(&f.since, "since", "", "Only logs newer than this, such as 15m, 2h or 7d")
	flags.StringVar(&f.start, "start", "", "Only logs at or after this RFC 3339 time")
	flags.StringVar(&f.end, "end", "", "Only logs before this RFC 3339 time")
}

// parse fills in the time window. now is passed in so --since is measured from
// one instant.
func (f *queryFlags) parse(now time.Time) error {
	f.selector.Type = strings.ToLower(strings.TrimSpace(f.selector.Type))
	f.selector.Kind = strings.ToLower(strings.TrimSpace(f.selector.Kind))
	f.filters.Level = strings.ToLower(strings.TrimSpace(f.filters.Level))
	names := f.selector.Names[:0]
	for _, name := range f.selector.Names {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	f.selector.Names = names

	if f.since != "" && f.start != "" {
		return errors.New("pass --since or --start, not both")
	}
	if f.since != "" {
		since, err := parseSince(f.since)
		if err != nil {
			return err
		}
		start := now.Add(-since)
		f.filters.Start = &start
	}
	for _, flag := range []struct {
		name  string
		value string
		into  **time.Time
	}{{"--start", f.start, &f.filters.Start}, {"--end", f.end, &f.filters.End}} {
		if flag.value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(flag.value))
		if err != nil {
			return fmt.Errorf("%s must be an RFC 3339 time, such as 2026-10-30T09:00:00Z", flag.name)
		}
		*flag.into = &parsed
	}
	return nil
}

// parseSince reads a Go duration, plus whole days such as 7d, which log
// retention is measured in.
func parseSince(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(value, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return time.Duration(n) * 24 * time.Hour, nil
		}
	}
	since, err := time.ParseDuration(value)
	if err != nil || since <= 0 {
		return 0, fmt.Errorf("--since must be a positive duration, such as 15m, 2h or 7d, not %q", value)
	}
	return since, nil
}

type searchOptions struct {
	deps         cliruntime.Deps
	query        queryFlags
	limit        int
	follow       bool
	jsonOutput   bool
	out          io.Writer
	errOut       io.Writer
	printNotices func()
}

// New returns the logs command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	opts := searchOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Search and tail the project's runtime logs",
		Long: fmt.Sprintf(`Search the current project's runtime logs, newest first, across every resource
of one type or the ones named with --name.

--type picks functions (the default), frontends or databases; one read covers
one type. Narrow it with --query, which takes quoted text, AND, OR, NOT,
parentheses, and fields such as level:error, and with --level, --region,
--invocation and a time window. History is limited to the plan's log
retention.

--follow tails new logs as they arrive, after printing up to --limit recent
ones. --json prints one JSON object per log, with its level, region, resource
and invocation.

Build logs stay on each resource:
  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "functions logs <name> --type build"),
			cliruntime.CommandPath(deps, "durable logs <name>"),
			cliruntime.CommandPath(deps, "frontends logs <name> --type build")),
		Example: strings.Join([]string{
			"  " + cliruntime.CommandPath(deps, "logs --since 1h --level error"),
			"  " + cliruntime.CommandPath(deps, `logs --name checkout --query 'timeout OR "connection reset"'`),
			"  " + cliruntime.CommandPath(deps, "logs --type frontend --name web --follow"),
			"  " + cliruntime.CommandPath(deps, "logs --type database --since 7d --json"),
		}, "\n"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out, opts.errOut = cmd.OutOrStdout(), cmd.ErrOrStderr()
			opts.printNotices = func() { upgradecmd.PrintAPIInstructionNotices(cmd, deps) }
			return runSearch(cmd.Context(), opts)
		},
	}
	opts.query.bind(cmd)
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", defaultLimit, "Maximum logs to print; with --follow, recent logs to print before tailing")
	cmd.Flags().BoolVarP(&opts.follow, "follow", "f", false, "Tail new logs as they arrive")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print one JSON object per log")
	cmd.AddCommand(newActivity(deps))
	return cmd
}

func runSearch(ctx context.Context, opts searchOptions) error {
	if opts.limit < 1 {
		return errors.New("--limit must be at least 1")
	}
	if err := opts.query.parse(time.Now()); err != nil {
		return err
	}
	if opts.follow && opts.query.filters.End != nil {
		return errors.New("--end cannot be combined with --follow: a tail has no end")
	}

	service := projectlog.NewService(opts.deps)
	query, err := service.Query(ctx, opts.query.selector, opts.query.filters)
	if err != nil {
		return err
	}

	if opts.follow {
		return logfollow.RuntimeEach(ctx, opts.deps, func(ctx context.Context, lastEventID string) (*api.ProjectLogStream, error) {
			return service.Stream(ctx, query, opts.limit, lastEventID)
		}, opts.printNotices, func(event *api.ProjectLogStreamEvent) error {
			return output.ProjectLogStreamEvent(opts.out, opts.errOut, event, opts.jsonOutput)
		})
	}

	printed := 0
	more, err := service.Each(ctx, query, opts.limit, func(events []apiclient.LogSearchEvent) error {
		printed += len(events)
		if opts.jsonOutput {
			return output.ProjectLogEventsJSON(opts.out, events)
		}
		output.ProjectLogEvents(opts.out, events)
		return nil
	})
	if err != nil {
		return err
	}
	// Notes go to stderr, so --json output stays one log per line.
	switch {
	case printed == 0:
		output.Note(opts.errOut, "No logs match.")
	case more:
		output.Note(opts.errOut, "More logs match. Raise --limit, or narrow the window with --since, --start or --end.")
	}
	return nil
}

type activityOptions struct {
	deps       cliruntime.Deps
	query      queryFlags
	buckets    int
	jsonOutput bool
	out        io.Writer
}

func newActivity(deps cliruntime.Deps) *cobra.Command {
	opts := activityOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Count the project's runtime logs over time",
		Long: `Count the current project's runtime logs in time buckets, with each bucket's
counts by level. It takes the same selection and filters as 'logs'. The window
defaults to the last 24 hours; --buckets sets how many buckets it is split into.

--json prints every bucket's counts by level, region and resource ID.`,
		Example: "  " + cliruntime.CommandPath(deps, "logs activity --since 6h") + "\n  " +
			cliruntime.CommandPath(deps, "logs activity --name checkout --level error --json"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runActivity(cmd.Context(), opts)
		},
	}
	opts.query.bind(cmd)
	cmd.Flags().IntVar(&opts.buckets, "buckets", defaultBuckets, fmt.Sprintf("Number of time buckets, at most %d", maxBuckets))
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runActivity(ctx context.Context, opts activityOptions) error {
	if opts.buckets < 1 || opts.buckets > maxBuckets {
		return fmt.Errorf("--buckets must be between 1 and %d", maxBuckets)
	}
	if err := opts.query.parse(time.Now()); err != nil {
		return err
	}

	service := projectlog.NewService(opts.deps)
	query, err := service.Query(ctx, opts.query.selector, opts.query.filters)
	if err != nil {
		return err
	}
	activity, err := service.Activity(ctx, query, opts.buckets)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, activity)
	}
	output.LogActivity(opts.out, activity, projectlog.Levels)
	return nil
}
