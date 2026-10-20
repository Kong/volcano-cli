// Package feedback implements `volcano feedback`, which coding agents run to
// report a problem they hit with Volcano. See package internal/feedback.
package feedback

import (
	"encoding/json"
	"fmt"
	"os"
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
	var input string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Report a problem you hit with Volcano",
		Long: `Send the Volcano team a report about a problem with Volcano: the CLI,
the platform, the docs, or the agent skills. Coding agents run this after a
task, so the user's work is not held up.

Give the report as flags, or as one JSON object with --input. --input takes
the object itself, a file that holds it, or - for stdin. The object's keys are
category, summary, details, commands (a list), expected, actual, and
workaround, with the same limits as the flags.

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

Write only what Volcano printed and what you did. Replace project, function,
database, and bucket names, file paths, and payload values with placeholders
such as <function> or <path>, and every password, token, or key with
<secret>. Do not include file contents, environment variable values,
application data, or customer or business names. The CLI replaces your home
directory with ~ and removes only Volcano tokens and keys, passwords in URLs,
and email addresses, so redact other secrets yourself.

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
    --workaround "Parsed the table output instead."

  # The same report as JSON: inline, from a file, or on stdin
  volcano feedback --input '{"category": "docs_mismatch", "summary": "...", "details": "..."}'
  volcano feedback --input report.json
  cat report.json | volcano feedback --input -`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if input != "" {
				var err error
				if in, err = readInput(cmd, input); err != nil {
					return err
				}
			}
			if err := in.Validate(); err != nil {
				return err
			}
			client, reporting := recorder.Client()
			if !reporting && !dryRun {
				fmt.Fprintln(cmd.ErrOrStderr(), "Feedback not sent: telemetry is off, or this is the first command since install. See `volcano telemetry --help`.")
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
	f.StringVar(&input, "input", "", "The report as a JSON object, a file that holds it, or - for stdin, instead of the flags above")
	f.BoolVar(&dryRun, "dry-run", false, "Print the report that would be sent, and send nothing")
	for _, name := range []string{"category", "summary", "details", "command", "expected", "actual", "workaround"} {
		cmd.MarkFlagsMutuallyExclusive("input", name)
	}
	return cmd
}

// readInput parses the JSON report given to --input: the object itself when
// the value starts with "{", stdin when it is "-", and otherwise the file it
// names.
func readInput(cmd *cobra.Command, value string) (feedback.Input, error) {
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		return feedback.ParseInput(strings.NewReader(value))
	}
	if value == "-" {
		return feedback.ParseInput(cmd.InOrStdin())
	}
	f, err := os.Open(value)
	if err != nil {
		return feedback.Input{}, err
	}
	defer func() { _ = f.Close() }()
	return feedback.ParseInput(f)
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
