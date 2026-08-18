// Package branch wires the volcano databases branches subcommands.
package branch

import (
	"errors"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the branches command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "branches",
		Aliases: []string{"branch"},
		Short:   "Manage database branches",
		Long: `Fork a database into a short-lived branch you can develop and test against.

A branch starts as a copy of its parent and diverges from there: writes to a
branch never reach the parent. Every branch expires, and only the storage it
diverges by counts against the parent database's allowance.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newExtend(deps))
	cmd.AddCommand(newReset(deps))
	cmd.AddCommand(newRotatePassword(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}

// parseTarget validates the "<database> <branch>" arguments every per-branch
// command takes.
func parseTarget(args []string) (databaseName, branchName string, err error) {
	databaseName = strings.TrimSpace(args[0])
	branchName = strings.TrimSpace(args[1])
	if databaseName == "" {
		return "", "", errors.New("database name cannot be empty")
	}
	if branchName == "" {
		return "", "", errors.New("branch name cannot be empty")
	}
	return databaseName, branchName, nil
}

// ttlSeconds converts a --ttl duration to the whole seconds the API takes.
func ttlSeconds(ttl time.Duration) int64 {
	return int64(ttl / time.Second)
}
