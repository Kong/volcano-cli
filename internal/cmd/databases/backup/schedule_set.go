package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type scheduleSetOptions struct {
	deps     cliruntime.Deps
	database string
	entries  []apiclient.DatabaseBackupScheduleEntry
	clear    bool
	out      io.Writer
}

func newScheduleSet(deps cliruntime.Deps) *cobra.Command {
	var frequency string
	var hour int
	var day int
	var retention time.Duration
	var clearSchedule bool
	cmd := &cobra.Command{
		Use:   "set <database>",
		Short: "Set a database's backup schedule",
		Long: fmt.Sprintf(`Replace a database's backup schedule.

The schedule is replaced wholesale, so this sets when the database is backed up
rather than adding another recurrence. Use --clear to stop scheduled backups.

Retention is clamped to the plan's, so the schedule printed back can keep
backups for less time than asked for.

Examples:
  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases backup-schedule set app --frequency daily --hour 3"),
			cliruntime.CommandPath(deps, "databases backup-schedule set app --frequency weekly --day 1 --hour 4 --retention 720h"),
			cliruntime.CommandPath(deps, "databases backup-schedule set app --clear")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := parseDatabase(args)
			if err != nil {
				return err
			}
			entries, err := scheduleEntries(scheduleFlags{
				frequency:    frequency,
				hour:         hour,
				day:          day,
				daySet:       cmd.Flags().Changed("day"),
				retention:    retention,
				retentionSet: cmd.Flags().Changed("retention"),
				clear:        clearSchedule,
			})
			if err != nil {
				return err
			}
			return runScheduleSet(cmd.Context(), scheduleSetOptions{
				deps:     deps,
				database: database,
				entries:  entries,
				clear:    clearSchedule,
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&frequency, "frequency", "", "How often to back up: daily, weekly, or monthly")
	cmd.Flags().IntVar(&hour, "hour", 0, "Hour of the day in UTC, 0-23")
	cmd.Flags().IntVar(&day, "day", 0, "Day of the week (1-7, Monday to Sunday) for weekly, or day of the month (1-28) for monthly")
	cmd.Flags().DurationVar(&retention, "retention", 0, "How long to keep each backup (defaults to the plan's retention)")
	cmd.Flags().BoolVar(&clearSchedule, "clear", false, "Stop scheduled backups")
	cmd.MarkFlagsMutuallyExclusive("clear", "frequency")
	cmd.MarkFlagsMutuallyExclusive("clear", "hour")
	cmd.MarkFlagsMutuallyExclusive("clear", "day")
	cmd.MarkFlagsMutuallyExclusive("clear", "retention")
	cmd.MarkFlagsOneRequired("clear", "frequency")
	return cmd
}

type scheduleFlags struct {
	frequency    string
	hour         int
	day          int
	daySet       bool
	retention    time.Duration
	retentionSet bool
	clear        bool
}

// Bounds the API enforces, checked here so a typo is a usage error rather than
// a round trip.
const (
	maxScheduleHour     = 23
	minScheduleWeekday  = 1
	maxScheduleWeekday  = 7
	minScheduleMonthDay = 1
	maxScheduleMonthDay = 28
)

// scheduleEntries turns the flags into the schedule to send. --clear sends an
// empty schedule, which is how the API stops scheduled backups.
func scheduleEntries(flags scheduleFlags) ([]apiclient.DatabaseBackupScheduleEntry, error) {
	if flags.clear {
		return []apiclient.DatabaseBackupScheduleEntry{}, nil
	}

	frequency := strings.ToLower(strings.TrimSpace(flags.frequency))
	if flags.hour < 0 || flags.hour > maxScheduleHour {
		return nil, fmt.Errorf("--hour must be between 0 and %d", maxScheduleHour)
	}

	entry := apiclient.DatabaseBackupScheduleEntry{
		Frequency: apiclient.DatabaseBackupScheduleEntryFrequency(frequency),
		Hour:      flags.hour,
	}
	if err := applyScheduleDay(&entry, frequency, flags); err != nil {
		return nil, err
	}
	if flags.retentionSet {
		seconds := int64(flags.retention / time.Second)
		entry.RetentionSeconds = &seconds
	}
	return []apiclient.DatabaseBackupScheduleEntry{entry}, nil
}

func applyScheduleDay(entry *apiclient.DatabaseBackupScheduleEntry, frequency string, flags scheduleFlags) error {
	switch frequency {
	case "daily":
		if flags.daySet {
			return errors.New("--day does not apply to a daily schedule")
		}
		return nil
	case "weekly":
		// Required, unlike before: the API numbers the week from Monday, so
		// there is no day left for an unset flag to mean.
		if !flags.daySet || flags.day < minScheduleWeekday || flags.day > maxScheduleWeekday {
			return fmt.Errorf("--day must be between %d (Monday) and %d (Sunday) for a weekly schedule",
				minScheduleWeekday, maxScheduleWeekday)
		}
		day := flags.day
		entry.Day = &day
		return nil
	case "monthly":
		if !flags.daySet || flags.day < minScheduleMonthDay || flags.day > maxScheduleMonthDay {
			return fmt.Errorf("--day must be between %d and %d for a monthly schedule", minScheduleMonthDay, maxScheduleMonthDay)
		}
		day := flags.day
		entry.Day = &day
		return nil
	default:
		return errors.New("--frequency must be daily, weekly, or monthly")
	}
}

func runScheduleSet(ctx context.Context, opts scheduleSetOptions) error {
	schedule, err := clidatabase.NewService(opts.deps).SetBackupSchedule(ctx, opts.database, opts.entries)
	if err != nil {
		return err
	}

	if opts.clear {
		output.Success(opts.out, "Scheduled backups of database '%s' stopped", opts.database)
		return nil
	}
	output.Success(opts.out, "Backup schedule of database '%s' updated", opts.database)
	output.DatabaseBackupSchedule(opts.out, schedule, opts.database)
	return nil
}
