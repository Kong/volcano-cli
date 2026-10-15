// Package feedback implements `volcano feedback`, which coding agents run to
// report a problem they hit with Volcano. See package internal/feedback.
package feedback

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/feedback"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

// recorder is the process's telemetry recorder, which decides whether
// feedback may be sent. main sets it with SetRecorder so the process reads the
// telemetry setting once; the zero value reads the real config.
var recorder = &telemetry.Recorder{}

// SetRecorder makes the command use the process's telemetry recorder.
func SetRecorder(r *telemetry.Recorder) { recorder = r }

// New returns the feedback command.
func New() *cobra.Command {
	var in feedback.Input
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Report a problem you hit with Volcano",
		Long: `Send the Volcano team a report about a problem with Volcano: the CLI,
the platform, the docs, or the agent skills. Coding agents run this after a
task, so the user's work is not held up.

Report:
  cli_bug               the CLI crashed, returned the wrong exit code, or printed
                        an error that names the wrong cause
  docs_mismatch         docs, skills, or --help disagree with what the CLI or
                        platform does
  missing_capability    no command does what the task needed, so you edited
                        files by hand or called the API directly
  platform_error        a server error that happens again on retry
  confusing_ux          you got it right only after several wrong attempts
  instruction_conflict  two Volcano instruction sources say different things

Do not report bugs in the user's own code, typos, a guessed flag you did not
check in --help, a network error that worked on retry, documented behavior
(volcano login waiting for browser approval, rate limits), or style
preferences.

Write only what Volcano printed and what you did. Do not include file
contents, environment variable values, application data, or customer or
business names. Before sending, the CLI replaces your home directory with ~
and removes tokens, keys, and passwords, as it does for usage data.

Each report also carries the CLI version, OS, architecture, the coding agent,
whether the Volcano plugin is installed, the selected project ID, and this
machine's telemetry ID. Reports are linked to your account when you are logged
in. Feedback is off when telemetry is off (volcano telemetry --help); the
command then sends nothing and exits 0.`,
		Example: `  volcano feedback \
    --category docs_mismatch \
    --summary "volcano-platform skill passes --json to functions list; the CLI rejects it" \
    --details "Listing functions to check a deploy, I ran the command the skill shows." \
    --command "volcano functions list --json" \
    --expected "JSON list of functions" \
    --actual "Error: unknown flag: --json" \
    --workaround "Parsed the table output instead."`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := in.Validate(); err != nil {
				return err
			}
			client, reporting := recorder.Client()
			if !reporting && !dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), "Feedback not sent: telemetry is off. See `volcano telemetry --help`.")
				return nil
			}
			report := feedback.Build(in, client, recorder.Home(), time.Now())
			if dryRun {
				return printReport(cmd, report, reporting)
			}
			payload, err := json.Marshal(report)
			if err != nil {
				return err
			}
			// Always the cloud API: a local-mode config points at the local
			// server, which does not take feedback.
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := feedback.Send(cmd.Context(), nil, cfg.APIURL(), cfg.Token(), payload); err != nil {
				return fmt.Errorf("send feedback: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Feedback sent. ID: %s\n", report.FeedbackID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.Category, "category", "", "Kind of problem: "+strings.Join(feedback.Categories, ", ")+" (required)")
	f.StringVar(&in.Summary, "summary", "", fmt.Sprintf("One line describing the problem, up to %d characters (required)", feedback.MaxSummary))
	f.StringVar(&in.Details, "details", "", fmt.Sprintf("What you were doing and what happened, up to %d characters (required)", feedback.MaxDetails))
	f.StringArrayVar(&in.Commands, "command", nil, fmt.Sprintf("A command you ran that shows the problem; repeat for each, up to %d", feedback.MaxCommands))
	f.StringVar(&in.Expected, "expected", "", fmt.Sprintf("What you expected, up to %d characters", feedback.MaxExpected))
	f.StringVar(&in.Actual, "actual", "", fmt.Sprintf("What happened, with the exact error text, up to %d characters", feedback.MaxActual))
	f.StringVar(&in.Workaround, "workaround", "", fmt.Sprintf("What you did instead, up to %d characters", feedback.MaxWorkaround))
	f.BoolVar(&dryRun, "dry-run", false, "Print the report that would be sent, and send nothing")
	return cmd
}

func printReport(cmd *cobra.Command, report feedback.Report, reporting bool) error {
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	if !reporting {
		fmt.Fprintln(cmd.ErrOrStderr(), "Telemetry is off, so this report would not be sent.")
	}
	return nil
}
