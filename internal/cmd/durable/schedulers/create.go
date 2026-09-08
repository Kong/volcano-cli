package schedulers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps     cliruntime.Deps
	function string
	name     string
	cron     string
	input    string
	regions  string
	out      io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create <function>",
		Short: "Create a durable function scheduler",
		Long: `Create a scheduler that starts an execution of a durable function on a cron
schedule.

Each tick starts an execution under a name derived from that run, so a tick
Volcano has to retry resolves to the execution it already started rather than
beginning a second one.

The --cron flag accepts standard 5-field cron expressions such as "*/5 * * * *".
If --regions is omitted, Volcano chooses one deployed region and keeps using it
until geofencing removes that region. If --regions is provided, it must be a
single region where the function is deployed.

The --input flag accepts either inline JSON or a path to a JSON file, and is the
input every execution the scheduler starts receives.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, `durable schedulers create order-pipeline --cron "0 * * * *"`),
			cliruntime.CommandPath(deps, `durable schedulers create order-pipeline --name nightly-sweep --cron "0 2 * * *" --input sweep.json`),
			cliruntime.CommandPath(deps, `durable schedulers create order-pipeline --cron "0 9 * * 1-5" --regions aws-us-east-1`)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runCreate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.cron, "cron", "", "5-field cron expression, for example '0 * * * *' (required)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Scheduler name (defaults to '<function> scheduler')")
	cmd.Flags().StringVar(&opts.input, "input", "", "Inline JSON object or path to a JSON file passed as each execution's input")
	cmd.Flags().StringVar(&opts.regions, "regions", "", "Single scheduler region; defaults to one deployed region")
	_ = cmd.MarkFlagRequired("cron")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	input := api.FunctionSchedulerInput{
		Name:           strings.TrimSpace(opts.name),
		CronExpression: strings.TrimSpace(opts.cron),
	}
	if input.Name == "" {
		input.Name = opts.function + " scheduler"
	}
	if value := strings.TrimSpace(opts.input); value != "" {
		payload, err := cmdutil.ParseJSONObject("input", value)
		if err != nil {
			return err
		}
		input.Payload = payload
	}
	if regions := strings.TrimSpace(opts.regions); regions != "" {
		input.Regions = []string{regions}
	}

	scheduler, err := clidurable.NewService(opts.deps).CreateScheduler(ctx, opts.function, input)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Created scheduler for durable function %q", opts.function)
	return nil
}
