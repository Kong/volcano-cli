package frontends

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
	errOut     io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a frontend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				out:        cmd.OutOrStdout(),
				errOut:     cmd.ErrOrStderr(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	service := clifrontend.NewService(opts.deps)
	frontend, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}
	// The function listing only names the route targets, so a failed one
	// leaves the routes showing function IDs rather than failing the command.
	routes, err := service.RouteTargets(ctx, frontend.FunctionRoutes)
	if err != nil {
		output.Warning(opts.errOut, "routes on '%s' show function IDs instead of names: %v", frontend.Name, err)
		routes = make([]clifrontend.Route, 0, len(frontend.FunctionRoutes))
		for _, route := range frontend.FunctionRoutes {
			routes = append(routes, clifrontend.Route{FrontendFunctionRoute: route})
		}
	}

	// Likewise the frontend listing only names the mounted frontends.
	mounts, err := service.MountTargets(ctx, frontend.FrontendRoutes)
	if err != nil {
		output.Warning(opts.errOut, "Frontend routes on '%s' show frontend IDs instead of names: %v", frontend.Name, err)
		mounts = make([]clifrontend.Mount, 0, len(frontend.FrontendRoutes))
		for _, route := range frontend.FrontendRoutes {
			mounts = append(mounts, clifrontend.Mount{FrontendRoute: route})
		}
	}

	output.Frontend(opts.out, frontend, append(routeEntries(routes), mountEntries(mounts)...))
	return nil
}
