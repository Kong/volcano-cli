package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type settingsSetOptions struct {
	deps              cliruntime.Deps
	autonomy          string
	reviewPaths       []string
	clearReviewPaths  bool
	customContext     string
	customContextFile string
	systemPrompt      string
	systemPromptFile  string
	ungatedWorkflows  string
	yes               bool
	jsonOutput        bool
	in                io.Reader
	out               io.Writer
	errOut            io.Writer
}

func newSettingsSet(deps cliruntime.Deps) *cobra.Command {
	opts := settingsSetOptions{}
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Change LavaFlow's settings",
		Long: fmt.Sprintf(`Change LavaFlow's settings for the current project. A setting you don't pass
keeps its value. Only the project's owner can, from their own session: a
project access token is refused.

--autonomy decides how far LavaFlow goes: %[1]s diagnoses issues and leaves
any fix to a person; %[2]s also opens fix pull requests for review; %[4]s
also merges a fix once its review and the base branch's required checks pass,
where '%[10]s' shows merging available. %[5]s
--autonomy %[4]s prints that and asks first; --yes skips the question.

--human-review-path names a path whose changes always hold a fix for a
person; repeat it for each, up to %[6]d. It replaces the saved paths, and
--clear-human-review-paths removes them all.

--custom-context tells LavaFlow's runs about the project, and --system-prompt
adds instructions to LavaFlow's own. LavaFlow saves both, but its runs don't
use them yet. Each takes up to %[3]d characters, from the flag or a file
(--custom-context-file, --system-prompt-file). Pass an empty value to clear
one.

--allow-ungated-workflows: %[8]s %[9]s

%[7]s
It prints that and asks first; --yes skips the question. false turns it off
without asking. If LavaFlow is no longer available to the project,
--allow-ungated-workflows false also turns LavaFlow off.`, apiclient.DiagnoseOnly, apiclient.OpenPr, clilavaflow.MaxTextLength, apiclient.Merge,
			clilavaflow.MergeDisclosure, clilavaflow.MaxHumanReviewPaths, ungatedWorkflowsDisclosure,
			clilavaflow.UngatedWorkflowsSetting, clilavaflow.UngatedWorkflowsRules, cliruntime.CommandPath(deps, "lavaflow status")),
		Example: "  " + cliruntime.CommandPath(deps, "lavaflow settings set --autonomy diagnose_only") + "\n" +
			"  " + cliruntime.CommandPath(deps, "lavaflow settings set --custom-context-file lavaflow-context.md"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			update, err := settingsUpdate(cmd, opts)
			if err != nil {
				return err
			}
			return runSettingsSet(cmd.Context(), opts, update)
		},
	}
	cmd.Flags().StringVar(&opts.autonomy, "autonomy", "", "How far LavaFlow goes: diagnose_only, open_pr or merge")
	cmd.Flags().StringArrayVar(&opts.reviewPaths, "human-review-path", nil,
		"A path whose changes always hold a fix for a person (repeatable; replaces the saved paths)")
	cmd.Flags().BoolVar(&opts.clearReviewPaths, "clear-human-review-paths", false,
		"Remove every path that always holds a fix for a person")
	cmd.Flags().StringVar(&opts.customContext, "custom-context", "", "Context about the project for LavaFlow's runs")
	cmd.Flags().StringVar(&opts.customContextFile, "custom-context-file", "", "Read the custom context from a file")
	cmd.Flags().StringVar(&opts.systemPrompt, "system-prompt", "", "Instructions added to LavaFlow's own")
	cmd.Flags().StringVar(&opts.systemPromptFile, "system-prompt-file", "", "Read the system prompt from a file")
	cmd.Flags().StringVar(&opts.ungatedWorkflows, "allow-ungated-workflows", "",
		clilavaflow.UngatedWorkflowsSetting+" Pass true or false.")
	cmd.MarkFlagsMutuallyExclusive("custom-context", "custom-context-file")
	cmd.MarkFlagsMutuallyExclusive("system-prompt", "system-prompt-file")
	cmd.MarkFlagsMutuallyExclusive("human-review-path", "clear-human-review-paths")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

// settingsUpdate sends only the settings the flags name, so the rest keep
// their values.
func settingsUpdate(cmd *cobra.Command, opts settingsSetOptions) (apiclient.LavaflowSettingsUpdate, error) {
	var update apiclient.LavaflowSettingsUpdate
	flags := cmd.Flags()
	if flags.Changed("autonomy") {
		autonomy := apiclient.LavaflowAutonomy(opts.autonomy)
		update.Autonomy = &autonomy
	}
	switch {
	case flags.Changed("human-review-path"):
		paths := opts.reviewPaths
		update.MergePolicy = &apiclient.LavaflowMergePolicyUpdate{HumanReviewPaths: &paths}
	case opts.clearReviewPaths:
		update.MergePolicy = &apiclient.LavaflowMergePolicyUpdate{HumanReviewPaths: &[]string{}}
	}
	for _, text := range []struct {
		setting          **string
		flag, file       string
		value, valueFile string
	}{
		{&update.CustomContext, "custom-context", "custom-context-file", opts.customContext, opts.customContextFile},
		{&update.SystemPrompt, "system-prompt", "system-prompt-file", opts.systemPrompt, opts.systemPromptFile},
	} {
		switch {
		case flags.Changed(text.flag):
			*text.setting = &text.value
		case flags.Changed(text.file):
			content, err := os.ReadFile(text.valueFile)
			if err != nil {
				return update, fmt.Errorf("failed to read %s: %w", text.valueFile, err)
			}
			value := string(content)
			*text.setting = &value
		}
	}
	if flags.Changed("allow-ungated-workflows") {
		switch opts.ungatedWorkflows {
		case "true", "false":
			allow := opts.ungatedWorkflows == "true"
			update.AllowUngatedWorkflows = &allow
		default:
			return update, fmt.Errorf("invalid --allow-ungated-workflows %q: use true or false", opts.ungatedWorkflows)
		}
	}
	if update.Autonomy == nil && update.MergePolicy == nil && update.CustomContext == nil && update.SystemPrompt == nil &&
		update.AllowUngatedWorkflows == nil {
		return update, errors.New("nothing to change: pass --autonomy, --human-review-path, --clear-human-review-paths, " +
			"--custom-context, --system-prompt, or --allow-ungated-workflows")
	}
	return update, nil
}

func runSettingsSet(ctx context.Context, opts settingsSetOptions, update apiclient.LavaflowSettingsUpdate) error {
	service := clilavaflow.NewService(opts.deps)
	var settings *apiclient.LavaflowSettings
	var err error
	if turnsOffUngatedWorkflowsOnly(update) {
		var lavaflowOff bool
		settings, lavaflowOff, err = service.TurnOffUngatedWorkflows(ctx)
		if err == nil && lavaflowOff {
			_, _ = fmt.Fprintln(opts.errOut, ungatedWorkflowsOffTurnsLavaFlowOff)
		}
	} else {
		settings, err = service.UpdateSettings(ctx, update, confirmation(opts, update))
	}
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.LavaflowSettingsSaved(opts.out, "LavaFlow's settings are saved", settings)
	return nil
}

// ungatedWorkflowsOffTurnsLavaFlowOff is what settings set prints, on stderr,
// when turning ungated workflows off turned LavaFlow off too.
const ungatedWorkflowsOffTurnsLavaFlowOff = "LavaFlow isn't available for this project, and Volcano turns " +
	"ungated workflows off then only together with LavaFlow, so LavaFlow is off too."

// turnsOffUngatedWorkflowsOnly reports whether update changes nothing but
// turning ungated workflows off.
func turnsOffUngatedWorkflowsOnly(update apiclient.LavaflowSettingsUpdate) bool {
	return update.AllowUngatedWorkflows != nil && !*update.AllowUngatedWorkflows && update.Autonomy == nil &&
		update.MergePolicy == nil && update.CustomContext == nil && update.SystemPrompt == nil && update.Enabled == nil
}

// ungatedWorkflowsDisclosure is what settings set prints, and its help says,
// before it allows ungated workflows. The contract's words start without a
// subject, so the flag names it.
const ungatedWorkflowsDisclosure = "--allow-ungated-workflows true: " + clilavaflow.UngatedWorkflowsDisclosure

// confirmation asks once before a change that turns on what only a person
// decides, autonomy merge, ungated workflows or both, after saying what each
// does. It's nil when the update turns on neither.
func confirmation(opts settingsSetOptions, update apiclient.LavaflowSettingsUpdate) func() error {
	merge := update.Autonomy != nil && *update.Autonomy == apiclient.Merge
	ungated := update.AllowUngatedWorkflows != nil && *update.AllowUngatedWorkflows
	var disclosure, question string
	switch {
	case merge && ungated:
		disclosure = clilavaflow.MergeDisclosure + "\n" + ungatedWorkflowsDisclosure
		question = "Let LavaFlow merge fixes, and allow ungated workflows?"
	case merge:
		disclosure, question = clilavaflow.MergeDisclosure, "Let LavaFlow merge fixes?"
	case ungated:
		disclosure, question = ungatedWorkflowsDisclosure, "Allow ungated workflows?"
	default:
		return nil
	}
	return func() error { return confirmChange(opts.yes, opts.in, opts.errOut, disclosure, question) }
}
