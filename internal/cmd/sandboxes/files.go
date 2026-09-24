package sandboxes

import (
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func (c *commands) files() *cobra.Command {
	cmd := &cobra.Command{Use: "files", Short: "Read or write session files"}
	cmd.AddCommand(&cobra.Command{Use: "read <session-id> <path>", Short: "Write file bytes to stdout", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		value, err := project.API.ReadSandboxFile(cmd.Context(), id, apiclient.SandboxFileReadRequest{Path: args[1]})
		if err != nil {
			return err
		}
		if c.json {
			return c.write(cmd, value)
		}
		_, err = cmd.OutOrStdout().Write(value.Data)
		return err
	}})
	cmd.AddCommand(&cobra.Command{Use: "write <session-id> <path>", Short: "Read file bytes from stdin", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		id, err := uuid.Parse(args[0])
		if err != nil {
			return err
		}
		const limit = 8 << 20
		data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), limit+1))
		if err != nil {
			return err
		}
		if len(data) > limit {
			return errors.New("files are limited to 8 MiB")
		}
		project, err := c.project()
		if err != nil {
			return err
		}
		if err := project.API.WriteSandboxFile(cmd.Context(), id, apiclient.SandboxFileWriteRequest{Path: args[1], Data: data}); err != nil {
			return err
		}
		return c.write(cmd, struct {
			Path string `json:"path"`
		}{args[1]})
	}})
	return cmd
}
