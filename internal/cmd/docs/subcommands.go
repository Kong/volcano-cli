package docs

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/docs"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newSync(deps cliruntime.Deps, f *flags) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch or refresh the local docs cache from the source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := f.service(deps)
			if err != nil {
				return failOrErr(cmd, f, "sync", nil, err)
			}
			if f.offline {
				err := fmt.Errorf("%w: cannot sync with --offline", docs.ErrSourceUnavailable)
				return failOrErr(cmd, f, "sync", svc, err)
			}
			res, err := svc.Sync(cmd.Context(), force)
			if err != nil {
				return failOrErr(cmd, f, "sync", svc, err)
			}
			if f.jsonOut {
				return emitJSON(cmd, "sync", svc, false, res.ResolvedCommit, res)
			}
			out := cmd.OutOrStdout()
			if !res.Changed {
				output.Success(out, "Docs cache already up to date (commit %s)", shortCommit(res.ResolvedCommit))
			} else {
				output.Success(out, "Synced docs at commit %s", shortCommit(res.ResolvedCommit))
				fmt.Fprintf(out, "  added %d, updated %d, removed %d, unchanged %d\n",
					res.Added, res.Updated, res.Removed, res.Unchanged)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Re-download all docs even if unchanged")
	return cmd
}

func newSearch(deps cliruntime.Deps, f *flags) *cobra.Command {
	var (
		limit int
		topic string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the docs and rank matching sections",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			svc, err := f.service(deps)
			if err != nil {
				return failOrErr(cmd, f, "search", nil, err)
			}
			results, err := svc.Search(cmd.Context(), query, topic, limit, f.offline)
			if err != nil {
				return failOrErr(cmd, f, "search", svc, err)
			}
			if f.jsonOut {
				return emitJSON(cmd, "search", svc, f.offline, "", results)
			}
			warnStale(cmd, svc, f.offline)
			renderSearch(cmd, query, results)
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 10, "Maximum number of results")
	cmd.Flags().StringVar(&topic, "topic", "", "Restrict to a docs topic (top-level directory)")
	return cmd
}

func newGet(deps cliruntime.Deps, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <path[#anchor]>",
		Short: "Print a document, or a single section when an #anchor is given",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := f.service(deps)
			if err != nil {
				return failOrErr(cmd, f, "get", nil, err)
			}
			res, err := svc.Get(cmd.Context(), args[0], f.offline)
			if err != nil {
				return failOrErr(cmd, f, "get", svc, err)
			}
			if f.jsonOut {
				return emitJSON(cmd, "get", svc, f.offline, "", res)
			}
			warnStale(cmd, svc, f.offline)
			fmt.Fprint(cmd.OutOrStdout(), res.Content)
			if !strings.HasSuffix(res.Content, "\n") {
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}
	return cmd
}

func newList(deps cliruntime.Deps, f *flags) *cobra.Command {
	var topic string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available documents",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := f.service(deps)
			if err != nil {
				return failOrErr(cmd, f, "list", nil, err)
			}
			items, err := svc.List(cmd.Context(), topic, f.offline)
			if err != nil {
				return failOrErr(cmd, f, "list", svc, err)
			}
			if f.jsonOut {
				return emitJSON(cmd, "list", svc, f.offline, "", items)
			}
			warnStale(cmd, svc, f.offline)
			renderList(cmd, items)
			return nil
		},
	}
	cmd.Flags().StringVar(&topic, "topic", "", "Restrict to a docs topic (top-level directory)")
	return cmd
}

// failOrErr routes an error to a JSON envelope (stdout) in --json mode, or
// returns it for the root to render on stderr otherwise.
func failOrErr(cmd *cobra.Command, f *flags, command string, svc *docs.Service, err error) error {
	if f.jsonOut {
		return failJSON(cmd, command, svc, f.offline, err)
	}
	return err
}

func renderSearch(cmd *cobra.Command, query string, results []docs.Result) {
	out := cmd.OutOrStdout()
	if len(results) == 0 {
		fmt.Fprintf(out, "No docs matched %q\n", query)
		return
	}
	for _, r := range results {
		title := strings.Join(r.HeadingPath, " › ")
		if title == "" {
			title = r.Title
		}
		fmt.Fprintf(out, "%2d. %s\n", r.Rank, title)
		fmt.Fprintf(out, "    id: %s\n", r.ID)
		if r.Snippet != "" {
			fmt.Fprintf(out, "    %s\n", r.Snippet)
		}
	}
	fmt.Fprintf(out, "\nShowing %d result(s). Read one with:\n  volcano docs get \"<id>\"\n", len(results))
}

func renderList(cmd *cobra.Command, items []docs.ListItem) {
	out := cmd.OutOrStdout()
	if len(items) == 0 {
		fmt.Fprintln(out, "No documents in cache")
		return
	}
	for _, it := range items {
		fmt.Fprintf(out, "%-48s %s\n", it.Path, it.Title)
	}
	fmt.Fprintf(out, "\n%d document(s)\n", len(items))
}

func shortCommit(c string) string {
	if len(c) > 8 {
		return c[:8]
	}
	return c
}
