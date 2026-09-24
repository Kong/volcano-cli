// Package sandboxes provides local and cloud Sandbox commands.
package sandboxes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/session"
)

// ExitError preserves the guest command's exit status.
type ExitError struct{ Code int }

func (e *ExitError) Error() string {
	return fmt.Sprintf("Sandbox command exited with status %d", e.Code)
}

// ExitCode returns a portable process status.
func (e *ExitError) ExitCode() int {
	if e.Code < 1 || e.Code > 255 {
		return 1
	}
	return e.Code
}

type commands struct {
	deps cliruntime.Deps
	json bool
}

// New returns the Sandbox command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	c := &commands{deps: deps}
	cmd := &cobra.Command{Use: "sandboxes", Short: "Run isolated commands and manage temporary sessions"}
	cmd.PersistentFlags().BoolVar(&c.json, "json", false, "Print JSON output")
	cmd.AddCommand(c.presets(), c.execute(), c.run(), c.sessions(), c.get(), c.control("suspend"), c.control("resume"), c.control("terminate"), c.files(), c.templates())
	return cmd
}

func (c *commands) project() (*session.ProjectSession, error) {
	deps := c.deps
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: time.Hour + 3*time.Minute}
	}
	return session.NewFactory(deps).CurrentProject()
}

func (c *commands) write(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	if !c.json {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func (c *commands) presets() *cobra.Command {
	return &cobra.Command{Use: "presets", Short: "List available preset images", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.SandboxPresets(cmd.Context())
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
}

func (c *commands) sessions() *cobra.Command {
	var cursor string
	cmd := &cobra.Command{Use: "sessions", Short: "List sessions in the selected project", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		project, err := c.project()
		if err != nil {
			return err
		}
		params := &apiclient.ListSandboxSessionsParams{}
		if cursor != "" {
			params.Cursor = &cursor
		}
		value, err := project.API.SandboxSessions(cmd.Context(), project.ProjectID, params)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from a returned pagination cursor")
	return cmd
}

func (c *commands) get() *cobra.Command {
	return &cobra.Command{Use: "get <session-id>", Short: "Read session state", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.SandboxSession(cmd.Context(), id)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
}

func (c *commands) control(action string) *cobra.Command {
	return &cobra.Command{Use: action + " <session-id>", Short: action + " a session", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		var value *apiclient.SandboxSession
		switch action {
		case "suspend":
			value, err = project.API.SuspendSandbox(cmd.Context(), id)
		case "resume":
			value, err = project.API.ResumeSandbox(cmd.Context(), id)
		case "terminate":
			value, err = project.API.TerminateSandbox(cmd.Context(), id)
		default:
			return errors.New("unknown Sandbox action")
		}
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
}
