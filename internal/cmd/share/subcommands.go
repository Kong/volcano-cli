package share

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sharing"
)

type linkOptions struct {
	deps       cliruntime.Deps
	linkID     string
	jsonOutput bool
	out        io.Writer
}

func linkFlags(cmd *cobra.Command, opts *linkOptions) {
	cmd.Flags().StringVar(&opts.linkID, "link", "", "Share to use, by its link or id, when the project has several (default: its newest)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print the result as JSON")
}

// statusResult is volcano share status, as JSON under --json.
type statusResult struct {
	Share   *apiclient.Share        `json:"share"`
	Release *apiclient.ShareRelease `json:"release"`
	Stats   *apiclient.ShareStats   `json:"stats"`
	URL     string                  `json:"url"`
}

func newStatus(deps cliruntime.Deps) *cobra.Command {
	opts := linkOptions{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the current cloud project is shared, and how often it is forked",
		Long: `Show how the current cloud project is shared: whether sharing is on and its link
serves the project, its link, description and whether copies may be shared
again, the release people fork, and how many people opened the link and forked
it in the last 30 days.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runStatus(cmd.Context(), opts)
		},
	}
	linkFlags(cmd, &opts)
	return cmd
}

func runStatus(ctx context.Context, opts linkOptions) error {
	service := sharing.NewService(opts.deps)
	share, err := requireShare(ctx, service, opts.linkID)
	if err != nil {
		return err
	}
	var release *apiclient.ShareRelease
	if share.CurrentRelease != nil {
		// Publishing makes the new release the current one, so the current
		// release is the newest.
		latest, found, err := service.Latest(ctx, share.Id)
		if err != nil {
			return err
		}
		if found && latest.Id == share.CurrentRelease.Id {
			release = &latest
		}
	}
	stats, err := service.Stats(ctx, share.Id, 0)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, statusResult{Share: share, Release: release, Stats: stats, URL: output.ShareURL(share.Id)})
	}
	output.ShareStatus(opts.out, share, release)
	fmt.Fprintln(opts.out)
	output.ShareStats(opts.out, share.Id, stats)
	return nil
}

func newOff(deps cliruntime.Deps) *cobra.Command {
	opts := linkOptions{}
	cmd := &cobra.Command{
		Use:   "off",
		Short: "Stop sharing the current cloud project",
		Long: `Stop sharing the current cloud project. Its link stops offering the project, and
forks it started that have not finished fail at their next step. Copies already
made keep working. You can always switch sharing off, even when your account is
read-only; 'volcano share' switches it on again.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runOff(cmd.Context(), opts)
		},
	}
	linkFlags(cmd, &opts)
	return cmd
}

func runOff(ctx context.Context, opts linkOptions) error {
	service := sharing.NewService(opts.deps)
	share, err := requireShare(ctx, service, opts.linkID)
	if err != nil {
		return err
	}
	share, err = service.SetStatus(ctx, share.Id, apiclient.ShareUpdateRequestStatusDisabled)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, share)
	}
	output.Success(opts.out, "Sharing is off. %s no longer offers the project", output.ShareURL(share.Id))
	fmt.Fprintf(opts.out, "Share it again with: %s\n", cliruntime.CommandPath(cliruntime.Deps{}, "share"))
	return nil
}

func newReleases(deps cliruntime.Deps) *cobra.Command {
	opts := linkOptions{}
	cmd := &cobra.Command{
		Use:   "releases",
		Short: "List what the current cloud project has shared",
		Long: `List the releases the current cloud project has shared, newest first. The newest
is the one people fork. --json has each release's functions, frontends,
variables, service key functions and what it leaves out.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runReleases(cmd.Context(), opts)
		},
	}
	linkFlags(cmd, &opts)
	return cmd
}

func runReleases(ctx context.Context, opts linkOptions) error {
	service := sharing.NewService(opts.deps)
	share, err := requireShare(ctx, service, opts.linkID)
	if err != nil {
		return err
	}
	releases, err := service.Releases(ctx, share.Id)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		if releases == nil {
			releases = []apiclient.ShareRelease{}
		}
		return writeJSON(opts.out, releases)
	}
	output.ShareReleases(opts.out, releases)
	return nil
}
