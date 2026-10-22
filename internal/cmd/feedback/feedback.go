// Package feedback implements `volcano feedback`, which coding agents run to
// report a problem they hit with Volcano, and its status, enable, and disable
// subcommands. See package internal/feedback.
package feedback

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/feedback"
	"github.com/Kong/volcano-cli/internal/sensitive"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

// recorder is the process's telemetry recorder, which decides whether
// feedback may be sent and what the report says about the machine. main sets
// it with SetRecorder so the process reads the telemetry setting once; the
// zero value reads the real config.
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
task.

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
directory with ~ and removes bearer tokens, JWTs, Volcano tokens and keys,
passwords in URLs, email addresses, and URL query strings. It removes nothing
else, so redact other secrets yourself.

Each report also carries the CLI version, OS, architecture, the coding agent,
and whether the Volcano plugin is installed. While telemetry is on, it also
carries the selected project ID and this machine's telemetry ID, and is linked
to your account. With telemetry off and feedback turned on with volcano
feedback enable, it carries neither and is not linked to your account. Every
report is sent with your login, which the server checks, so sending one
requires volcano login. A project access token cannot send reports.

Feedback follows telemetry: turning telemetry off also turns feedback off,
unless you run volcano feedback enable. Turn it off with volcano feedback
disable.`,
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
		// A stray argument is likely report text given without a flag, so
		// usage telemetry does not send the error that quotes it.
		Args: func(cmd *cobra.Command, args []string) error {
			return sensitive.Wrap(cobra.NoArgs(cmd, args))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The recorder also turns feedback off when the config cannot be
			// read. Return that error instead of sending nothing without a
			// word. A project link that cannot be used does not stop a
			// report, which then leaves out the project ID.
			cfg, _, err := config.LoadIgnoringBadLink()
			if err != nil {
				return err
			}
			// Checked before the report is read, so feedback that is off
			// never fails on the report.
			on := recorder.FeedbackOn()
			if !on && !dryRun {
				return nil
			}
			// The server takes reports only from a logged-in account, so a
			// logged-out run, or one with a project access token, sends
			// nothing, like feedback that is off.
			token := cfg.Token()
			loggedIn := telemetry.CanSendFeedback(token)
			if !loggedIn && !dryRun {
				return nil
			}
			if input != "" {
				if in, err = readInput(cmd, input); err != nil {
					return err
				}
			}
			if err := in.Validate(); err != nil {
				return err
			}
			// While telemetry is off, the report is anonymous: the server
			// checks the login but does not attribute the report to the user.
			report := feedback.Build(in, recorder.Client(), recorder.Home(), time.Now())
			if err := report.Validate(); err != nil {
				return fmt.Errorf("after redaction, %w", err)
			}
			if dryRun {
				return printReport(cmd, report, on && loggedIn)
			}
			payload, err := encodeReport(report, "")
			if err != nil {
				return err
			}
			if telemetry.FeedbackDebugByEnv(os.Getenv) {
				if err := saveReport(payload); err != nil {
					return fmt.Errorf("save feedback: %w", err)
				}
			} else {
				// Always the cloud API, also in local mode: reports go to
				// the Volcano team, not the local server.
				if err := feedback.Send(cmd.Context(), nil, cfg.APIURL(), token, payload); err != nil {
					return fmt.Errorf("send feedback: %w", err)
				}
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
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the CLI sends feedback reports",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printStatus(cmd)
			},
		},
		&cobra.Command{
			Use:   "enable",
			Short: "Send feedback reports",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setEnabled(cmd, true)
			},
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Stop sending feedback reports",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setEnabled(cmd, false)
			},
		},
	)
	return cmd
}

func printStatus(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	if telemetry.FeedbackDisabledByEnv(os.Getenv) {
		fmt.Fprintf(w, "Feedback is disabled by %s.\n", telemetry.EnvFeedbackDisabled)
		return nil
	}
	// The setting lives only in ~/.volcano/config.json, so a broken project
	// link in the working directory does not stop status from reading it.
	cfg, err := config.LoadWithoutLink()
	if err != nil {
		return err
	}
	switch telemetry.FeedbackSetting(cfg, os.Getenv) {
	case telemetry.FeedbackOffByUser:
		fmt.Fprintln(w, "Feedback is disabled. Run `volcano feedback enable` to turn it on.")
		return nil
	case telemetry.FeedbackOffWithTelemetry:
		fmt.Fprintln(w, "Feedback is disabled because telemetry is off. Run `volcano feedback enable` to turn it on.")
		return nil
	}
	if cfg.Token() == "" {
		fmt.Fprintln(w, "Feedback is enabled, but no report is sent until you log in with `volcano login`.")
		return nil
	}
	if config.IsProjectToken(cfg.Token()) {
		fmt.Fprintln(w, "Feedback is enabled, but a project access token cannot send reports. Log in with `volcano login`.")
		return nil
	}
	// A report is sent only once the first-run notice is saved. On a fresh
	// machine this command shows and saves it, so the next report is sent;
	// in a home that cannot be saved, no report ever is.
	if !recorder.AnnounceFeedback(cmd.ErrOrStderr()) {
		fmt.Fprintln(w, "Feedback is enabled, but no report is sent until the first-run notice can be saved to ~/.volcano/config.json.")
		return nil
	}
	fmt.Fprintln(w, "Feedback is enabled. Run `volcano feedback disable` to turn it off.")
	return nil
}

// setEnabled saves the user's feedback choice, which from then on applies
// whatever the telemetry setting is.
func setEnabled(cmd *cobra.Command, enabled bool) error {
	err := config.Update(func(cfg *config.Config) (bool, error) {
		if cfg.Feedback == nil {
			cfg.Feedback = &config.FeedbackConfig{}
		}
		cfg.Feedback.Enabled = &enabled
		// Choosing a setting means the user has seen what feedback is.
		cfg.Feedback.NoticeShown = true
		return true, nil
	})
	if err != nil {
		return err
	}
	if enabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Feedback is enabled.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Feedback is disabled.")
	}
	if enabled && telemetry.FeedbackDisabledByEnv(os.Getenv) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s is set, so no feedback is sent until it is unset.\n", telemetry.EnvFeedbackDisabled)
	}
	return nil
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
		// The error quotes value, which can be report text that was not
		// given as an object, so usage telemetry does not send it.
		return feedback.Input{}, fmt.Errorf("read input: %w", sensitive.Wrap(err))
	}
	defer func() { _ = f.Close() }()
	return feedback.ParseInput(f)
}

// encodeReport encodes report as JSON, indented by indent. It leaves "<", ">",
// and "&" as written, so placeholders such as <function> read the same in the
// --dry-run preview and on the server.
func encodeReport(report feedback.Report, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(report); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// saveReport appends payload as one line to feedback.jsonl next to the config,
// for VOLCANO_FEEDBACK_DEBUG.
func saveReport(payload []byte) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	path = filepath.Join(filepath.Dir(path), "feedback.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(payload, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// printReport prints report for --dry-run, and says so when a real run would
// send nothing: feedback is off, the first-run notice has not been shown, or
// the user is logged out or uses a project access token.
func printReport(cmd *cobra.Command, report feedback.Report, sends bool) error {
	out, err := encodeReport(report, "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(out))
	if !sends {
		fmt.Fprintln(cmd.ErrOrStderr(), "This report would not be sent.")
	}
	return nil
}
