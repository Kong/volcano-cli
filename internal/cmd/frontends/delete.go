// Package frontends wires the volcano frontends subcommands.
package frontends

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps       cliruntime.Deps
	identifier string
	yes        bool
	in         io.Reader
	out        io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Delete a frontend",
		Long: `Delete a frontend from the current project.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				yes:        yes,
				in:         cmd.InOrStdin(),
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	service := clifrontend.NewService(opts.deps)
	frontend, err := service.Resolve(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "frontend", frontend.Name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.DeleteByID(ctx, frontend.Id); err != nil {
		return withMountedHint(ctx, opts.deps, service, frontend.Id, err)
	}

	output.Success(opts.out, "Frontend '%s' deletion started", frontend.Name)
	fmt.Fprintln(opts.out, `Status will be "deleting" until cleanup finishes; afterwards the frontend will no longer appear in list/get responses.`)
	return nil
}

// withMountedHint adds the commands that remove the Frontend routes mounting a
// frontend to the 409 the server answers its delete with. The server's message
// already names the mounting frontends, so a failed lookup leaves it as is.
func withMountedHint(ctx context.Context, deps cliruntime.Deps, service clifrontend.Service, frontendID uuid.UUID, err error) error {
	if api.Status(err) != http.StatusConflict {
		return err
	}
	mounts, lookupErr := service.MountedBy(ctx, frontendID)
	if lookupErr != nil || len(mounts) == 0 {
		return err
	}
	commands := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		commands = append(commands, "  "+cliruntime.CommandPath(deps, fmt.Sprintf("frontends routes delete %s %s", mount.Frontend, mount.PathPrefix)))
	}
	return fmt.Errorf("%w\nremove the routes that mount it first:\n%s", err, strings.Join(commands, "\n"))
}
