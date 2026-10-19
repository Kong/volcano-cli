package notifications

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	clinotification "github.com/Kong/volcano-cli/internal/notification"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type rotateOptions struct {
	deps       cliruntime.Deps
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
}

func newRotateWebhookSecret(deps cliruntime.Deps) *cobra.Command {
	opts := rotateOptions{}
	cmd := &cobra.Command{
		Use:   "rotate-webhook-secret",
		Short: "Replace the webhook signing secret",
		Long: `Replace the current project's webhook signing secret. The new secret is
printed once, in this command's output.

Every delivery from now on is signed with the new secret, including retries of
earlier alerts, so update your receiver right away.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			return runRotate(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON, including the new signing secret")
	return cmd
}

func runRotate(ctx context.Context, opts rotateOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"A receiver that checks the current signing secret will reject deliveries until it has the new one.",
			"Rotate the webhook signing secret?")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	settings, err := clinotification.NewService(opts.deps).RotateWebhookSecret(ctx)
	if api.Status(err) == http.StatusConflict {
		return fmt.Errorf("%w\nSet a webhook first: %s", err,
			cliruntime.CommandPath(opts.deps, "notifications set --webhook-url <url>"))
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.Success(opts.out, "Webhook signing secret rotated")
	output.NotificationSettings(opts.out, settings)
	return nil
}
