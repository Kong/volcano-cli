// Package cmdutil contains shared Cobra command helpers.
package cmdutil

import (
	"fmt"

	"github.com/spf13/cobra"
)

// HideDeprecatedAlias hides cmd from help and prints warning before legacy paths run.
func HideDeprecatedAlias(cmd *cobra.Command, warning string) *cobra.Command {
	cmd.Hidden = true
	addDeprecatedAliasWarning(cmd, warning)
	return cmd
}

func addDeprecatedAliasWarning(cmd *cobra.Command, warning string) {
	if cmd.Run != nil || cmd.RunE != nil {
		preRun := cmd.PreRun
		preRunE := cmd.PreRunE
		cmd.PreRun = nil
		cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
			if preRunE != nil {
				return preRunE(cmd, args)
			}
			if preRun != nil {
				preRun(cmd, args)
			}
			return nil
		}
	}
	for _, child := range cmd.Commands() {
		addDeprecatedAliasWarning(child, warning)
	}
}
