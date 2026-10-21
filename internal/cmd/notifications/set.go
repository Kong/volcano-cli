package notifications

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clinotification "github.com/Kong/volcano-cli/internal/notification"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type setOptions struct {
	deps          cliruntime.Deps
	update        clinotification.Update
	removeWebhook bool
	jsonOutput    bool
	out           io.Writer
}

func newSet(deps cliruntime.Deps) *cobra.Command {
	opts := setOptions{}
	var (
		email            bool
		recipients       []string
		webhookURL       string
		notifyOn         string
		threshold        int
		notifyOnRecovery bool
	)
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change notification settings",
		Long: fmt.Sprintf(`Change the current project's notification settings. Settings you do not pass
keep their current values.

Setting a webhook on a project without one generates its signing secret, which
is printed once, in this command's output. Use it to verify that a delivery
came from Volcano.

Alert emails go to the project owner's account email address, the only
recipient supported for now. Send alerts anywhere else through a webhook.

Examples:
  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "notifications set --webhook-url https://example.com/hooks/volcano"),
			cliruntime.CommandPath(deps, "notifications set --notify-on first_failure"),
			cliruntime.CommandPath(deps, "notifications set --email=false --remove-webhook")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cmd.Flags()
			if flags.Changed("email") {
				opts.update.EmailEnabled = &email
			}
			if flags.Changed("recipients") {
				opts.update.Recipients = &recipients
			}
			if flags.Changed("webhook-url") {
				opts.update.WebhookURL = &webhookURL
			}
			if flags.Changed("notify-on") {
				opts.update.NotifyOn = &notifyOn
			}
			if flags.Changed("threshold") {
				opts.update.Threshold = &threshold
			}
			if flags.Changed("notify-on-recovery") {
				opts.update.NotifyOnRecovery = &notifyOnRecovery
			}
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runSet(cmd.Context(), opts)
		},
	}
	flags := cmd.Flags()
	flags.BoolVar(&email, "email", true, "Email alerts; --email=false turns email off")
	flags.StringSliceVar(&recipients, "recipients", nil,
		"Only the project owner's account email address is supported for now, which is also the default; any other address is refused")
	flags.StringVar(&webhookURL, "webhook-url", "", "Post alerts to this http or https URL")
	flags.BoolVar(&opts.removeWebhook, "remove-webhook", false, "Stop posting alerts to the webhook and delete its signing secret")
	flags.StringVar(&notifyOn, "notify-on", "",
		fmt.Sprintf("When a failing scheduler is announced (%s)", strings.Join(clinotification.NotifyOnValues(), " or ")))
	flags.IntVar(&threshold, "threshold", 0,
		fmt.Sprintf("Failed runs in a row before a %s alert (%d-%d)", clinotification.NotifyOnConsecutiveFailures, clinotification.MinThreshold, clinotification.MaxThreshold))
	flags.BoolVar(&notifyOnRecovery, "notify-on-recovery", true, "Alert when an announced failing scheduler succeeds again")
	flags.BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON, including a newly generated signing secret")
	cmd.MarkFlagsMutuallyExclusive("webhook-url", "remove-webhook")
	return cmd
}

func runSet(ctx context.Context, opts setOptions) error {
	if opts.update.WebhookURL != nil && strings.TrimSpace(*opts.update.WebhookURL) == "" {
		return errors.New("--webhook-url needs a URL; use --remove-webhook to remove the webhook")
	}
	if opts.removeWebhook {
		removed := ""
		opts.update.WebhookURL = &removed
	}
	if opts.update.Empty() {
		return errors.New("nothing to change: pass at least one setting, such as --webhook-url or --notify-on")
	}

	settings, err := clinotification.NewService(opts.deps).Update(ctx, opts.update)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.Success(opts.out, "Notification settings saved")
	output.NotificationSettings(opts.out, settings)
	return nil
}
