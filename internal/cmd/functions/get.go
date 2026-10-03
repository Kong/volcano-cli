package functions

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a function",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	service := clifunction.NewService(opts.deps)
	function, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}
	sources, err := service.RoutedFrom(ctx, function.Id)
	if err != nil {
		return err
	}

	routedFrom := make([]output.FunctionRouteSource, 0, len(sources))
	for _, source := range sources {
		routedFrom = append(routedFrom, output.FunctionRouteSource{Frontend: source.Frontend, PathPrefix: source.PathPrefix})
	}
	output.Function(opts.out, function, routedFrom)
	return nil
}
