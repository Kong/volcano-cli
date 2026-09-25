package sandboxes

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
)

func (c *commands) templates() *cobra.Command {
	cmd := &cobra.Command{Use: "templates", Short: "Manage named Sandbox presets"}
	cmd.AddCommand(c.listTemplates(), c.deleteTemplate())
	cmd.AddCommand(&cobra.Command{Use: "get <template-id>", Short: "Get a template", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.SandboxTemplate(cmd.Context(), project.ProjectID, id)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}})
	var preset, key string
	var memory int
	create := &cobra.Command{Use: "create <name>", Short: "Save a named preset template", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := requestID(key)
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		size := apiclient.CreateSandboxTemplateRequestMemoryMb(memory)
		value, err := project.API.CreateSandboxTemplate(cmd.Context(), project.ProjectID, id, apiclient.CreateSandboxTemplateRequest{Name: args[0], Preset: apiclient.CreateSandboxTemplateRequestPreset(preset), MemoryMb: &size})
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	create.Flags().StringVar(&preset, "preset", "python3.12", "Preset image")
	create.Flags().IntVar(&memory, "memory", 1024, "Memory in MB")
	create.Flags().StringVar(&key, "request-id", "", "UUID idempotency key")
	cmd.AddCommand(create)
	return cmd
}

func (c *commands) listTemplates() *cobra.Command {
	var cursor string
	cmd := &cobra.Command{Use: "list", Short: "List templates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		project, err := c.project()
		if err != nil {
			return err
		}
		params := &apiclient.ListSandboxesParams{}
		if cursor != "" {
			params.Cursor = &cursor
		}
		value, err := project.API.SandboxTemplates(cmd.Context(), project.ProjectID, params)
		if err != nil {
			return err
		}
		return c.write(cmd, value)
	}}
	cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from a returned pagination cursor")
	return cmd
}

func (c *commands) deleteTemplate() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "delete <template-id>", Short: "Delete a template and terminate its sessions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		if !yes {
			accepted, confirmErr := confirm.Action(cmd.InOrStdin(), cmd.ErrOrStderr(), "Deleting this template terminates all of its sessions and permanently removes their files.", fmt.Sprintf("Delete Sandbox template %s?", id))
			if confirmErr != nil {
				return confirmErr
			}
			if !accepted {
				return nil
			}
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		if err := project.API.DeleteSandboxTemplate(cmd.Context(), project.ProjectID, id); err != nil {
			return err
		}
		return c.write(cmd, struct {
			ID uuid.UUID `json:"id"`
		}{id})
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm termination of the template's sessions and deletion of their files")
	return cmd
}
