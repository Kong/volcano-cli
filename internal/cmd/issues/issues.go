// Package issues wires the volcano issues subcommands.
package issues

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const groupShort = "Read the project's issues and leave notes for LavaFlow"

const groupLong = `Volcano groups the runtime errors your functions and frontends log into
issues: each is one error type thrown from one place in your code, or one
message when there is no stack. List them, read one, and see how often and
when it happened.`

const notesLong = `

A note on an issue is context for LavaFlow: its next run on the issue reads
the issue's notes. Only the project's owner writes notes, as a person.`

// New returns the issues command for a cloud project.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := newGroup(groupLong + notesLong)
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newNotes(deps))
	return cmd
}

// NewLocal returns the issues command for local development, which records
// issues as the cloud does. Notes are cloud-only: they are input to LavaFlow,
// which local development doesn't run.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	cmd := newGroup(groupLong + `

Notes on issues are cloud-only: run 'volcano cloud issues notes ...'.`)
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newCloudOnlyNotes())
	return cmd
}

func newGroup(long string) *cobra.Command {
	return &cobra.Command{
		Use:   "issues",
		Short: groupShort,
		Long:  long,
		// An unknown subcommand is an error, not the group's help.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
}

// newCloudOnlyNotes stands in for the notes command in the local tree. Notes
// are input to LavaFlow, which runs on Volcano's cloud service, so local
// development has none. Hidden and without flag parsing, so every form of the
// command, its subcommands and flags included, gets the same answer.
func newCloudOnlyNotes() *cobra.Command {
	return &cobra.Command{
		Use:   "notes",
		Short: "Notes on an issue (cloud only)",
		Long: `Notes on an issue are input to LavaFlow, which runs on Volcano's cloud service and
not in local development, so this command is cloud-only: run
'volcano cloud issues notes ...' instead.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if slices.ContainsFunc(args, func(arg string) bool { return arg == "--help" || arg == "-h" }) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: notes are input to LavaFlow, which runs on Volcano's cloud "+
				"service and not in local development, so run 'volcano cloud issues notes ...' instead", "issues notes")
		},
	}
}
