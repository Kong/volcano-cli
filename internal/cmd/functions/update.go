package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type updateOptions struct {
	deps       cliruntime.Deps
	identifier string
	public     bool
	private    bool
	out        io.Writer
}

func newUpdate(deps cliruntime.Deps) *cobra.Command {
	var public bool
	var private bool
	cmd := &cobra.Command{
		Use:   "update <name-or-id>",
		Short: "Update function settings",
		Long: `Update function settings.

Currently supported:
  --public / --private to control invocation visibility.

Use exactly one of:
  --public   Make function publicly invokable
  --private  Require authenticated/service invocation`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "functions update hello --public"),
			cliruntime.CommandPath(deps, "functions update hello --private"),
			cliruntime.CommandPath(deps, "functions update 62ec7ca5-1f8a-47b2-b8f8-78fd93cd8152 --public")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), updateOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				public:     public,
				private:    private,
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVar(&public, "public", false, "Set function visibility to public")
	cmd.Flags().BoolVar(&private, "private", false, "Set function visibility to private")
	return cmd
}

func runUpdate(ctx context.Context, opts updateOptions) error {
	if opts.public == opts.private {
		return errors.New("specify exactly one visibility flag: --public or --private")
	}

	updated, err := clifunction.NewService(opts.deps).UpdateVisibility(ctx, opts.identifier, opts.public)
	if err != nil {
		return err
	}

	visibility := "private"
	if updated.IsPublic {
		visibility = "public"
	}
	output.Success(opts.out, "Function '%s' visibility set to %s", updated.Name, visibility)
	return nil
}
