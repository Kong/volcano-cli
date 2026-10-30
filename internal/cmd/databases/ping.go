package databases

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/dataplane"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type pingOptions struct {
	deps    cliruntime.Deps
	service []clidatabase.Option
	long    string
	// noBranches refuses --branch where the database has no branches to test.
	noBranches bool
	branch     string
	name       string
	jsonOutput bool
	out        io.Writer
}

// pingResult is what --json prints for a successful connection test.
type pingResult struct {
	Database  string  `json:"database"`
	Branch    *string `json:"branch"`
	LatencyMS int64   `json:"latency_ms"`
}

const pingSummary = `Test that a database, or one of its branches, accepts queries: run SELECT 1
through the database API and report the round trip. A failed test exits
non-zero with the reason.`

// newPing tests a cloud database. The database API takes a service key rather
// than a sign-in, so the test sends the one cloud data-plane commands share.
func newPing(deps cliruntime.Deps) *cobra.Command {
	return newPingCommand(pingOptions{
		deps:    deps,
		service: []clidatabase.Option{clidatabase.WithPingTokenProvider(dataplane.NewService(deps).ServiceKeyForProject)},
		long: pingSummary + `

The database API takes a service key rather than your sign-in. The test uses
VOLCANO_TOKEN when that is a service key, and otherwise the project's
` + "`" + dataplane.CLIServiceKeyName + "`" + ` key, which function invoke and
storage commands share and which is created on first use. The key is never
printed.`,
	})
}

// newLocalPing tests a local database with deps whose session token is the
// local service key. Local development has no branches.
func newLocalPing(deps cliruntime.Deps) *cobra.Command {
	return newPingCommand(pingOptions{
		deps:       deps,
		noBranches: true,
		long:       pingSummary + "\n\nLocal development authenticates the test with its own service key.",
	})
}

func newPingCommand(opts pingOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ping <name>",
		Short:   "Test a database connection",
		Long:    opts.long,
		Example: pingExample(opts),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name, opts.out = strings.TrimSpace(args[0]), cmd.OutOrStdout()
			return runPing(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.branch, "branch", "", "Test this branch of the database instead")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func pingExample(opts pingOptions) string {
	example := "  " + cliruntime.CommandPath(opts.deps, "databases ping app")
	if opts.noBranches {
		return example + "\n  " + cliruntime.CommandPath(opts.deps, "databases ping app --json")
	}
	return example + "\n  " + cliruntime.CommandPath(opts.deps, "databases ping app --branch preview --json")
}

func runPing(ctx context.Context, opts pingOptions) error {
	opts.branch = strings.TrimSpace(opts.branch)
	if opts.branch != "" && opts.noBranches {
		return fmt.Errorf("local development has no database branches, so run '%s' against a cloud project",
			cliruntime.CommandPath(cliruntime.Deps{CommandPathPrefix: "volcano cloud"}, "databases ping "+opts.name+" --branch "+opts.branch))
	}
	latency, err := clidatabase.NewService(opts.deps, opts.service...).Ping(ctx, opts.name, opts.branch)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		result := pingResult{Database: opts.name, LatencyMS: latency.Milliseconds()}
		if opts.branch != "" {
			result.Branch = &opts.branch
		}
		return output.JSON(opts.out, result)
	}
	target := fmt.Sprintf("database '%s'", opts.name)
	if opts.branch != "" {
		target = fmt.Sprintf("branch '%s' of database '%s'", opts.branch, opts.name)
	}
	output.Success(opts.out, "Connected to %s in %s", target, latency.Round(time.Millisecond))
	return nil
}
