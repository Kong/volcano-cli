package notifications

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clinotification "github.com/Kong/volcano-cli/internal/notification"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sensitive"
	"github.com/Kong/volcano-cli/internal/theme"
)

type testOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newTest(deps cliruntime.Deps) *cobra.Command {
	opts := testOptions{}
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test notification",
		Long: `Send a test notification on every channel the current project has configured,
and report whether each one arrived. A webhook delivery is signed like an
alert. One test per project per minute.

The command fails when a delivery did not arrive.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runTest(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runTest(ctx context.Context, opts testOptions) error {
	result, err := clinotification.NewService(opts.deps).SendTest(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		if err := writeJSON(opts.out, result); err != nil {
			return err
		}
	} else {
		output.NotificationTestDelivered(opts.out, result)
	}
	return undeliveredError(result)
}

// undeliveredError reports every delivery that did not arrive, or nil.
func undeliveredError(result *apiclient.NotificationTestResult) error {
	var failures []string
	for _, delivery := range result.Deliveries {
		if delivery.Delivered {
			continue
		}
		reason := "no reason given"
		if delivery.Error != nil && strings.TrimSpace(*delivery.Error) != "" {
			reason = theme.StripControl(strings.TrimSpace(*delivery.Error))
		}
		failures = append(failures, fmt.Sprintf("test %s not delivered%s: %s",
			delivery.Channel, output.NotificationDeliveryStatus(delivery), reason))
	}
	if len(failures) == 0 {
		return nil
	}
	// The reasons come from the server and name the receiver, so they are shown
	// but kept out of telemetry.
	return sensitive.Wrap(errors.New(strings.Join(failures, "\n")))
}
