package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/oapi-codegen/nullable"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type settingsSetOptions struct {
	deps              cliruntime.Deps
	autonomy          string
	mergeMode         string
	instructions      string
	instructionsFile  string
	reviewPaths       []string
	clearReviewPaths  bool
	mergeWithoutCheck string
	ciBeforeApproval  string
	customContext     string
	customContextFile string
	systemPrompt      string
	systemPromptFile  string
	jsonOutput        bool
	in                io.Reader
	out               io.Writer
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
any fix to a person; %[2]s also opens fix pull requests for review; %[6]s,
where merging is available for the project, also merges them as the merge
mode allows.

--merge-mode decides when LavaFlow merges a fix at autonomy %[6]s:
  %[4]s (the default): %[8]s
  %[5]s: %[9]s
In every mode: %[10]s

--merge-instructions says, in your own words, which fixes need a person in
auto mode, such as "Billing changes and anything irreversible need a person".
LavaFlow judges each fix against them, and they only add holds. Up to %[3]d characters, from the flag
or a file (--merge-instructions-file; - reads stdin). Pass an empty value to
go back to Volcano's default instructions, which 'settings' prints.
%[11]s

--human-review-path names a path whose changes always wait for a person;
repeat it for each, up to %[7]d. It replaces the saved paths, and
--clear-human-review-paths removes them all. --allow-merge-without-checks
true lets LavaFlow merge where the repository requires no checks.
--allow-ci-before-approval true runs CI before a person approves; only %[5]s
allows it.

--custom-context tells LavaFlow's runs about the project, and --system-prompt
adds instructions to LavaFlow's own. LavaFlow saves both, but its runs don't
use them yet. Each takes up to %[3]d characters, from the flag or a file
(--custom-context-file, --system-prompt-file). Pass an empty value to clear
one.`, apiclient.DiagnoseOnly, apiclient.OpenPr, clilavaflow.MaxTextLength,
			apiclient.LavaflowHumanMergeMode, apiclient.Auto, apiclient.Merge, clilavaflow.MaxHumanReviewPaths,
			output.LavaflowMergeModeDescription(apiclient.LavaflowHumanMergeMode),
			output.LavaflowMergeModeDescription(apiclient.Auto), output.LavaflowMergeFacts,
			output.LavaflowMergeInstructionsNote),
		Example: "  " + cliruntime.CommandPath(deps, "lavaflow settings set --autonomy diagnose_only") + "\n" +
			"  " + cliruntime.CommandPath(deps, "lavaflow settings set --custom-context-file lavaflow-context.md"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			update, err := settingsUpdate(cmd, opts)
			if err != nil {
				return err
			}
			return runSettingsSet(cmd.Context(), opts, update)
		},
	}
	cmd.Flags().StringVar(&opts.autonomy, "autonomy", "", "How far LavaFlow goes: diagnose_only, open_pr or merge")
	cmd.Flags().StringVar(&opts.mergeMode, "merge-mode", "", fmt.Sprintf("When LavaFlow merges a fix: %s or %s",
		apiclient.LavaflowHumanMergeMode, apiclient.Auto))
	cmd.Flags().StringVar(&opts.instructions, "merge-instructions", "",
		"Which fixes need a person in auto mode, in your own words (empty: Volcano's default)")
	cmd.Flags().StringVar(&opts.instructionsFile, "merge-instructions-file", "", "Read the merge instructions from a file, or - for stdin")
	cmd.Flags().StringArrayVar(&opts.reviewPaths, "human-review-path", nil,
		"A path whose changes always wait for a person (repeatable; replaces the saved paths)")
	cmd.Flags().BoolVar(&opts.clearReviewPaths, "clear-human-review-paths", false,
		"Remove every path that always waits for a person")
	cmd.Flags().StringVar(&opts.mergeWithoutCheck, "allow-merge-without-checks", "",
		"Whether LavaFlow may merge where the repository requires no checks: true or false")
	cmd.Flags().StringVar(&opts.ciBeforeApproval, "allow-ci-before-approval", "",
		"Whether CI runs before a person approves, in auto only: true or false")
	cmd.Flags().StringVar(&opts.customContext, "custom-context", "", "Context about the project for LavaFlow's runs")
	cmd.Flags().StringVar(&opts.customContextFile, "custom-context-file", "", "Read the custom context from a file")
	cmd.Flags().StringVar(&opts.systemPrompt, "system-prompt", "", "Instructions added to LavaFlow's own")
	cmd.Flags().StringVar(&opts.systemPromptFile, "system-prompt-file", "", "Read the system prompt from a file")
	cmd.MarkFlagsMutuallyExclusive("custom-context", "custom-context-file")
	cmd.MarkFlagsMutuallyExclusive("system-prompt", "system-prompt-file")
	cmd.MarkFlagsMutuallyExclusive("merge-instructions", "merge-instructions-file")
	cmd.MarkFlagsMutuallyExclusive("human-review-path", "clear-human-review-paths")
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
	if slices.ContainsFunc(mergePolicyFlags, flags.Changed) || opts.clearReviewPaths {
		policy, err := mergePolicyUpdate(flags, opts)
		if err != nil {
			return update, err
		}
		update.MergePolicy = policy
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
	if update.Autonomy == nil && update.MergePolicy == nil && update.CustomContext == nil && update.SystemPrompt == nil {
		return update, errors.New("nothing to change: pass --autonomy, --merge-mode, --merge-instructions, --human-review-path, " +
			"--clear-human-review-paths, --allow-merge-without-checks, --allow-ci-before-approval, " +
			"--custom-context, or --system-prompt")
	}
	return update, nil
}

// mergePolicyFlags name the merge policy fields, apart from
// --clear-human-review-paths, which changes them only when true.
var mergePolicyFlags = []string{
	"merge-mode", "merge-instructions", "merge-instructions-file", "human-review-path",
	"allow-merge-without-checks", "allow-ci-before-approval",
}

// mergePolicyUpdate holds only the merge policy fields the flags name.
func mergePolicyUpdate(flags *pflag.FlagSet, opts settingsSetOptions) (*apiclient.LavaflowMergePolicyUpdate, error) {
	policy := &apiclient.LavaflowMergePolicyUpdate{}
	if flags.Changed("merge-mode") {
		mode := apiclient.LavaflowMergeMode(opts.mergeMode)
		policy.Mode = &mode
	}
	if flags.Changed("merge-instructions") || flags.Changed("merge-instructions-file") {
		instructions := opts.instructions
		if flags.Changed("merge-instructions-file") {
			var err error
			if instructions, err = readMergeInstructions(opts); err != nil {
				return nil, err
			}
		}
		// Empty instructions go back to Volcano's default ones, which is null.
		policy.Instructions = nullable.NewNullNullable[string]()
		if instructions != "" {
			policy.Instructions = nullable.NewNullableWithValue(instructions)
		}
	}
	switch {
	case flags.Changed("human-review-path"):
		paths := opts.reviewPaths
		policy.HumanReviewPaths = &paths
	case opts.clearReviewPaths:
		paths := []string{}
		policy.HumanReviewPaths = &paths
	}
	for _, allow := range []struct {
		setting     **bool
		flag, value string
	}{
		{&policy.AllowMergeWithoutChecks, "allow-merge-without-checks", opts.mergeWithoutCheck},
		{&policy.AllowCiBeforeApproval, "allow-ci-before-approval", opts.ciBeforeApproval},
	} {
		if !flags.Changed(allow.flag) {
			continue
		}
		switch allow.value {
		case "true", "false":
			value := allow.value == "true"
			*allow.setting = &value
		default:
			return nil, fmt.Errorf("invalid --%s %q: use true or false", allow.flag, allow.value)
		}
	}
	return policy, nil
}

func runSettingsSet(ctx context.Context, opts settingsSetOptions, update apiclient.LavaflowSettingsUpdate) error {
	settings, err := clilavaflow.NewService(opts.deps).UpdateSettings(ctx, update)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.LavaflowSettingsSaved(opts.out, "LavaFlow's settings are saved", settings)
	return nil
}

// readMergeInstructions reads --merge-instructions-file, where - is stdin.
func readMergeInstructions(opts settingsSetOptions) (string, error) {
	if opts.instructionsFile == "-" {
		content, err := io.ReadAll(opts.in)
		if err != nil {
			return "", fmt.Errorf("failed to read the merge instructions from stdin: %w", err)
		}
		return string(content), nil
	}
	content, err := os.ReadFile(opts.instructionsFile)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", opts.instructionsFile, err)
	}
	return string(content), nil
}
