// Package schedulers wires the volcano functions schedulers subcommands.
package schedulers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps     cliruntime.Deps
	function string
	name     string
	cron     string
	payload  string
	regions  string
	out      io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create <function>",
		Short: "Create a function scheduler",
		Long: `Create a scheduled invocation for a deployed cloud function.

The --cron flag accepts standard 5-field cron expressions such as "*/5 * * * *".
If --regions is omitted, Volcano chooses one deployed region and keeps using it
until geofencing removes that region. If --regions is provided, it must be a
single region where the function is deployed.

The --payload flag accepts either inline JSON or a path to a JSON file.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, `functions schedulers create hello --cron "*/5 * * * *"`),
			cliruntime.CommandPath(deps, `functions schedulers create hello --name refresh-cache --cron "0 * * * *" --payload payload.json`),
			cliruntime.CommandPath(deps, `functions schedulers create hello --cron "0 9 * * 1-5" --regions us-east-1`)),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runCreate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.cron, "cron", "", "5-field cron expression, for example '*/5 * * * *' (required)")
	cmd.Flags().StringVar(&opts.name, "name", "", "Scheduler name (defaults to '<function> scheduler')")
	cmd.Flags().StringVar(&opts.payload, "payload", "", "Inline JSON object or path to a JSON file passed as the invocation payload")
	cmd.Flags().StringVar(&opts.regions, "regions", "", "Single scheduler region; defaults to one deployed region")
	_ = cmd.MarkFlagRequired("cron")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	var payload map[string]any
	if rawPayload := strings.TrimSpace(opts.payload); rawPayload != "" {
		parsed, err := loadSchedulerPayload(rawPayload)
		if err != nil {
			return err
		}
		payload = parsed
	}

	service := clifunction.NewService(opts.deps)
	fn, err := service.Resolve(ctx, opts.function)
	if err != nil {
		return err
	}

	name := strings.TrimSpace(opts.name)
	if name == "" {
		name = fn.Name + " scheduler"
	}

	input := api.FunctionSchedulerInput{
		Name:           name,
		CronExpression: strings.TrimSpace(opts.cron),
		Payload:        payload,
	}
	if regions := strings.TrimSpace(opts.regions); regions != "" {
		input.Regions = []string{regions}
	}

	scheduler, err := service.CreateSchedulerByID(ctx, fn.Id, input)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Created scheduler for function %q", fn.Name)
	return nil
}

// loadSchedulerPayload parses the --payload value as inline JSON or as a path
// to a JSON file. The caller is responsible for not invoking it on empty input.
func loadSchedulerPayload(value string) (map[string]any, error) {
	data := []byte(value)
	if info, statErr := os.Stat(value); statErr == nil && !info.IsDir() {
		fileBytes, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("failed to read payload file %q: %w", value, err)
		}
		data = fileBytes
	}

	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("payload must be a JSON object: %w", err)
	}
	return payload, nil
}
