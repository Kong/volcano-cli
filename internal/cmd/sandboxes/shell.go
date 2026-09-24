package sandboxes

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func (c *commands) shell() *cobra.Command {
	var timeout int
	cmd := &cobra.Command{Use: "shell <session-id>", Short: "Run commands from stdin in an existing session", Long: "Run one shell command per line in an existing session. Files and background processes persist; shell variables and working-directory changes do not. Type exit or send EOF to detach without terminating the session. This command does not provide a PTY.", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(cmd.InOrStdin())
		scanner.Buffer(make([]byte, 4096), 8192)
		var lastExit error
		for {
			if _, err = fmt.Fprint(cmd.ErrOrStderr(), "sandbox> "); err != nil {
				return err
			}
			if !scanner.Scan() {
				break
			}
			command := strings.TrimSpace(scanner.Text())
			if command == "exit" {
				return lastExit
			}
			if command == "" {
				continue
			}
			key, keyErr := uuid.NewRandom()
			if keyErr != nil {
				return keyErr
			}
			result, runErr := project.API.ExecSandboxSession(cmd.Context(), id, key, apiclient.SandboxCommandRequest{Command: command, TimeoutSeconds: &timeout})
			if runErr != nil {
				return runErr
			}
			lastExit = c.outputCommand(cmd, result.Stdout, result.Stderr, result.ExitCode, result)
			var exited *ExitError
			if lastExit != nil && !errors.As(lastExit, &exited) {
				return lastExit
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			return scanErr
		}
		return lastExit
	}}
	cmd.Flags().IntVar(&timeout, "timeout", 60, "Deadline for each command in seconds")
	return cmd
}
