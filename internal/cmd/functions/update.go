package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type updateOptions struct {
	deps       cliruntime.Deps
	identifier string
	visibility cmdutil.VisibilityFlags
	out        io.Writer
}

func newUpdate(deps cliruntime.Deps) *cobra.Command {
	opts := updateOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update function settings",
		Long: `Update function settings.

--visibility sets who can invoke the function:
  private        Service keys and schedulers only. New functions start here.
  authenticated  Also your project's signed-in users.
  public         Also anon keys with functions.invoke, and frontend routes.

--public is the same as --visibility public.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "functions update hello --visibility authenticated"),
			cliruntime.CommandPath(deps, "functions update hello --visibility public"),
			cliruntime.CommandPath(deps, "functions update 62ec7ca5-1f8a-47b2-b8f8-78fd93cd8152 --visibility private")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.identifier = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runUpdate(cmd.Context(), &opts)
		},
	}
	opts.visibility.Register(cmd.Flags(), "Who can invoke the function: private, authenticated, or public")
	return cmd
}

func runUpdate(ctx context.Context, opts *updateOptions) error {
	visibility, ok, err := opts.visibility.Visibility()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("specify --visibility private, authenticated, or public")
	}

	updated, err := clifunction.NewService(opts.deps).UpdateVisibility(ctx, opts.identifier, visibility)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Function '%s' visibility set to %s", updated.Name, output.FunctionVisibility(updated.Visibility, updated.IsPublic))
	return nil
}
