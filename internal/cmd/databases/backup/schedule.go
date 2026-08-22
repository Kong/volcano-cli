package backup

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewSchedule returns the backup-schedule command.
func NewSchedule(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup-schedule",
		Short: "Manage a database's automated backups",
		Long: `Back a database up on a recurring schedule.

Scheduled backups do not count against the plan's backup allowance, but their
retention is clamped to it. They are listed alongside the backups you took, with
a source of scheduled, and can be restored and deleted the same way.`,
	}
	cmd.AddCommand(newScheduleGet(deps))
	cmd.AddCommand(newScheduleSet(deps))
	return cmd
}
