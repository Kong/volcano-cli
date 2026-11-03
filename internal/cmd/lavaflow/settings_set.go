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
is for a project where merging is available. %[5]s
--autonomy %[4]s prints that and asks first; --yes skips the question.

--human-review-path names a path whose changes always hold a fix for a
person; repeat it for each, up to %[6]d. It replaces the saved paths, and
--clear-human-review-paths removes them all.

--custom-context tells LavaFlow's runs about the project, and --system-prompt
adds instructions to LavaFlow's own. LavaFlow saves both, but its runs don't
use them yet. Each takes up to %[3]d characters, from the flag or a file
(--custom-context-file, --system-prompt-file). Pass an empty value to clear
one.`, apiclient.DiagnoseOnly, apiclient.OpenPr, clilavaflow.MaxTextLength, apiclient.Merge,
			clilavaflow.MergeDisclosure, clilavaflow.MaxHumanReviewPaths),
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
	if update.Autonomy == nil && update.MergePolicy == nil && update.CustomContext == nil && update.SystemPrompt == nil {
		return update, errors.New("nothing to change: pass --autonomy, --human-review-path, --clear-human-review-paths, " +
			"--custom-context, or --system-prompt")
	}
	return update, nil
}

func runSettingsSet(ctx context.Context, opts settingsSetOptions, update apiclient.LavaflowSettingsUpdate) error {
	var ask func() error
	if update.Autonomy != nil && *update.Autonomy == apiclient.Merge {
		ask = func() error {
			return confirmChange(opts.yes, opts.in, opts.errOut, clilavaflow.MergeDisclosure, "Let LavaFlow merge fixes?")
		}
	}
	settings, err := clilavaflow.NewService(opts.deps).UpdateSettings(ctx, update, ask)
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
