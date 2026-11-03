package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

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
	reviewPaths       []string
	clearReviewPaths  bool
	mergeWithoutCheck string
	ciBeforeApproval  string
	customContext     string
	customContextFile string
	systemPrompt      string
	systemPromptFile  string
	jsonOutput        bool
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
any fix to a person; %[2]s also opens fix pull requests for review; %[7]s,
where merging is available for the project, also merges them as the merge
mode allows.

--merge-mode decides when LavaFlow merges a fix at autonomy %[7]s: %[4]s, the
default, waits for a person to approve each merge; the other modes are %[5]s
and %[6]s. Only a person's approval through Volcano releases a merge that
waits for one; a review on GitHub doesn't. Reviewer concerns block merging in
every mode.

--human-review-path names a path whose changes always wait for a person;
repeat it for each, up to %[8]d. It replaces the saved paths, and
--clear-human-review-paths removes them all. --allow-merge-without-checks
true lets LavaFlow merge where the repository requires no checks.
--allow-ci-before-approval true runs CI before a person approves; only %[6]s
allows it.

--custom-context tells LavaFlow's runs about the project, and --system-prompt
adds instructions to LavaFlow's own. LavaFlow saves both, but its runs don't
use them yet. Each takes up to %[3]d characters, from the flag or a file
(--custom-context-file, --system-prompt-file). Pass an empty value to clear
one.`, apiclient.DiagnoseOnly, apiclient.OpenPr, clilavaflow.MaxTextLength,
			apiclient.LavaflowHumanMergeMode, apiclient.RiskBased, apiclient.Auto, apiclient.Merge, clilavaflow.MaxHumanReviewPaths),
		Example: "  " + cliruntime.CommandPath(deps, "lavaflow settings set --autonomy diagnose_only") + "\n" +
			"  " + cliruntime.CommandPath(deps, "lavaflow settings set --custom-context-file lavaflow-context.md"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			update, err := settingsUpdate(cmd, opts)
			if err != nil {
				return err
			}
			return runSettingsSet(cmd.Context(), opts, update)
		},
	}
	cmd.Flags().StringVar(&opts.autonomy, "autonomy", "", "How far LavaFlow goes: diagnose_only, open_pr or merge")
	cmd.Flags().StringVar(&opts.mergeMode, "merge-mode", "", fmt.Sprintf("When LavaFlow merges a fix: %s, %s or %s",
		apiclient.LavaflowHumanMergeMode, apiclient.RiskBased, apiclient.Auto))
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
	policy, err := mergePolicyUpdate(flags, opts)
	if err != nil {
		return update, err
	}
	if policy != (apiclient.LavaflowMergePolicyUpdate{}) {
		update.MergePolicy = &policy
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
		return update, errors.New("nothing to change: pass --autonomy, --merge-mode, --human-review-path, " +
			"--clear-human-review-paths, --allow-merge-without-checks, --allow-ci-before-approval, " +
			"--custom-context, or --system-prompt")
	}
	return update, nil
}

// mergePolicyUpdate holds only the merge policy fields the flags name.
func mergePolicyUpdate(flags *pflag.FlagSet, opts settingsSetOptions) (apiclient.LavaflowMergePolicyUpdate, error) {
	var policy apiclient.LavaflowMergePolicyUpdate
	if flags.Changed("merge-mode") {
		mode := apiclient.LavaflowMergeMode(opts.mergeMode)
		policy.Mode = &mode
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
			return policy, fmt.Errorf("invalid --%s %q: use true or false", allow.flag, allow.value)
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
