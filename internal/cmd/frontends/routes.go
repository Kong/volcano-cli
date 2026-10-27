package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type routesListOptions struct {
	deps     cliruntime.Deps
	frontend string
	out      io.Writer
}

type routesCreateOptions struct {
	deps     cliruntime.Deps
	frontend string
	input    clifrontend.RouteInput
	out      io.Writer
}

type routesUpdateOptions struct {
	deps     cliruntime.Deps
	frontend string
	route    string
	update   clifrontend.RouteUpdate
	out      io.Writer
}

type routesDeleteOptions struct {
	deps     cliruntime.Deps
	frontend string
	route    string
	yes      bool
	in       io.Reader
	out      io.Writer
}

func newRoutes(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routes",
		Short: "Manage frontend function and Frontend routes",
		Long: `Manage the paths of a frontend that are served by a function or by another
frontend.

A function route sends every request under a path prefix, such as
/api/session, to an HTTP-mode function, so the browser calls it on the
frontend's own origin. The function must be public: anyone who can load the
frontend can call it, so it has to authenticate its callers itself.

A Frontend route mounts another frontend of the project at a path prefix, such
as /docs, so one hostname serves several apps. The path is forwarded
unchanged, so the mounted frontend must serve its pages under the prefix.

Both kinds share one set of paths on a frontend, and the longest matching
prefix wins.`,
	}
	cmd.AddCommand(newRoutesList(deps))
	cmd.AddCommand(newRoutesCreate(deps))
	cmd.AddCommand(newRoutesUpdate(deps))
	cmd.AddCommand(newRoutesDelete(deps))
	return cmd
}

func newRoutesList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <frontend>",
		Short: "List a frontend's function and Frontend routes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutesList(cmd.Context(), routesListOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func newRoutesCreate(deps cliruntime.Deps) *cobra.Command {
	var input clifrontend.RouteInput
	cmd := &cobra.Command{
		Use:   "create <frontend>",
		Short: "Serve a frontend path from a function or another frontend",
		Long: `Serve every request under a path prefix of a frontend from a function, with
--function, or from another frontend of the project, with --frontend.

The function must be a public, HTTP-mode standard function in the same project.
With --strip-prefix the function sees the rest of the path, or / for the prefix
itself; without it, the function sees the full path.

A mounted frontend always sees the full path, so it must serve its pages under
the prefix, in matching page directories or with a matching basePath.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends routes create web --path /api/session --function session --strip-prefix"),
			cliruntime.CommandPath(deps, "frontends routes create web --path /docs --frontend docs")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutesCreate(cmd.Context(), routesCreateOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				input:    input,
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&input.PathPrefix, "path", "", "Path prefix to forward, such as /api/session")
	cmd.Flags().StringVar(&input.Function, "function", "", "Function name or ID to forward to")
	cmd.Flags().StringVar(&input.Frontend, "frontend", "", "Frontend name or ID to mount")
	cmd.Flags().BoolVar(&input.StripPrefix, "strip-prefix", false, "Remove the prefix from the path the function sees")
	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err)
	}
	cmd.MarkFlagsOneRequired("function", "frontend")
	cmd.MarkFlagsMutuallyExclusive("function", "frontend")
	return cmd
}

func newRoutesUpdate(deps cliruntime.Deps) *cobra.Command {
	var pathPrefix, function, frontend string
	var stripPrefix bool
	cmd := &cobra.Command{
		Use:   "update <frontend> <path-or-id>",
		Short: "Change a frontend function or Frontend route",
		Long: `Change a route's path prefix or target.

Name the route by its current path prefix or its ID. Flags you leave out keep
their current value. A function route takes --function and --strip-prefix, and
a Frontend route takes --frontend. A route keeps its kind: to serve a path from
the other kind of target, delete the route and create a new one.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends routes update web /api/session --function session-v2"),
			cliruntime.CommandPath(deps, "frontends routes update web /api/session --strip-prefix=false"),
			cliruntime.CommandPath(deps, "frontends routes update web /docs --frontend docs-v2")),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := routesUpdateOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				route:    strings.TrimSpace(args[1]),
				out:      cmd.OutOrStdout(),
			}
			if cmd.Flags().Changed("path") {
				opts.update.PathPrefix = &pathPrefix
			}
			if cmd.Flags().Changed("function") {
				opts.update.Function = &function
			}
			if cmd.Flags().Changed("frontend") {
				opts.update.Frontend = &frontend
			}
			if cmd.Flags().Changed("strip-prefix") {
				opts.update.StripPrefix = &stripPrefix
			}
			return runRoutesUpdate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&pathPrefix, "path", "", "New path prefix")
	cmd.Flags().StringVar(&function, "function", "", "Function name or ID to forward to")
	cmd.Flags().StringVar(&frontend, "frontend", "", "Frontend name or ID to mount")
	cmd.Flags().BoolVar(&stripPrefix, "strip-prefix", false, "Remove the prefix from the path the function sees")
	cmd.MarkFlagsMutuallyExclusive("function", "frontend")
	cmd.MarkFlagsMutuallyExclusive("strip-prefix", "frontend")
	return cmd
}

func newRoutesDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <frontend> <path-or-id>",
		Short: "Stop serving a frontend path from a function or another frontend",
		Long: `Delete a frontend function or Frontend route, named by its path prefix or ID.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutesDelete(cmd.Context(), routesDeleteOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				route:    strings.TrimSpace(args[1]),
				yes:      yes,
				in:       cmd.InOrStdin(),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runRoutesList(ctx context.Context, opts routesListOptions) error {
	frontend, routes, err := clifrontend.NewService(opts.deps).ListRoutes(ctx, opts.frontend)
	if err != nil {
		return err
	}
	output.FrontendRoutes(opts.out, frontend.Name, append(routeEntries(routes.Function), mountEntries(routes.Frontend)...))
	return nil
}

func runRoutesCreate(ctx context.Context, opts routesCreateOptions) error {
	if err := validatePathPrefix(opts.input.PathPrefix); err != nil {
		return err
	}
	if opts.input.Frontend != "" && opts.input.StripPrefix {
		return errors.New("--strip-prefix applies to function routes only: a mounted frontend always sees the full path")
	}
	frontend, route, err := clifrontend.NewService(opts.deps).CreateRoute(ctx, opts.frontend, opts.input)
	if err != nil {
		return withTargetHint(opts.deps, err)
	}
	if route.Frontend != nil {
		entry := mountEntry(*route.Frontend)
		output.Success(opts.out, "Route %s on frontend '%s' now serves frontend '%s'", entry.PathPrefix, frontend.Name, entry.Frontend)
		output.FrontendRoute(opts.out, entry)
		fmt.Fprintf(opts.out, "'%s' receives the full path, so it must serve its pages under %s.\n", entry.Frontend, entry.PathPrefix)
		return nil
	}
	entry := routeEntry(*route.Function)
	output.Success(opts.out, "Route %s on frontend '%s' now forwards to function '%s'", entry.PathPrefix, frontend.Name, entry.Function)
	output.FrontendRoute(opts.out, entry)
	fmt.Fprintf(opts.out, "Anyone who can load '%s' can call '%s' under %s; the function must authenticate its own callers.\n",
		frontend.Name, entry.Function, entry.PathPrefix)
	return nil
}

func runRoutesUpdate(ctx context.Context, opts routesUpdateOptions) error {
	if opts.update.PathPrefix == nil && opts.update.Function == nil && opts.update.Frontend == nil && opts.update.StripPrefix == nil {
		return errors.New("specify at least one of --path, --function, --frontend, or --strip-prefix")
	}
	if opts.update.PathPrefix != nil {
		if err := validatePathPrefix(*opts.update.PathPrefix); err != nil {
			return err
		}
	}
	frontend, route, err := clifrontend.NewService(opts.deps).UpdateRoute(ctx, opts.frontend, opts.route, opts.update)
	if err != nil {
		return withTargetHint(opts.deps, err)
	}
	entry := resultEntry(route)
	output.Success(opts.out, "Route %s on frontend '%s' updated", entry.PathPrefix, frontend.Name)
	output.FrontendRoute(opts.out, entry)
	return nil
}

func runRoutesDelete(ctx context.Context, opts routesDeleteOptions) error {
	service := clifrontend.NewService(opts.deps)
	frontend, route, err := service.ResolveRoute(ctx, opts.frontend, opts.route)
	if err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "route", fmt.Sprintf("%s on frontend %s", route.PathPrefix(), frontend.Name))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.DeleteRoute(ctx, frontend.Id, route); err != nil {
		return err
	}
	output.Success(opts.out, "Route %s deleted from frontend '%s'", route.PathPrefix(), frontend.Name)
	return nil
}

// routePathPrefix is the pattern the API accepts. Its request validator
// answers any other prefix with a bare "invalid request".
var routePathPrefix = regexp.MustCompile(`^/[^?#\\]*[^/?#\\]$`)

func validatePathPrefix(prefix string) error {
	if length := utf8.RuneCountInString(prefix); length >= 2 && length <= 512 && routePathPrefix.MatchString(prefix) {
		return nil
	}
	return fmt.Errorf("invalid --path %q: a path prefix starts with /, does not end with /, "+
		`has no "?", "#", or "\", and is 2 to 512 characters long`, prefix)
}

func withTargetHint(deps cliruntime.Deps, err error) error {
	var notPublic *clifrontend.TargetNotPublicError
	if !errors.As(err, &notPublic) {
		return err
	}
	return fmt.Errorf("%w\nmake '%s' public first: %s", err, notPublic.Function,
		cliruntime.CommandPath(deps, "functions update "+notPublic.Function+" --visibility public"))
}

func routeEntries(routes []clifrontend.Route) []output.FrontendRouteEntry {
	entries := make([]output.FrontendRouteEntry, 0, len(routes))
	for _, route := range routes {
		entries = append(entries, routeEntry(route))
	}
	return entries
}

func routeEntry(route clifrontend.Route) output.FrontendRouteEntry {
	entry := output.FrontendRouteEntry{
		ID:          route.Id.String(),
		PathPrefix:  route.PathPrefix,
		StripPrefix: route.StripPrefix,
		Function:    route.FunctionId.String(),
	}
	if route.Function != nil {
		entry.Function = route.Function.Name
		entry.Visibility = output.FunctionVisibility(route.Function.Visibility, route.Function.IsPublic)
	}
	return entry
}

func mountEntries(mounts []clifrontend.Mount) []output.FrontendRouteEntry {
	entries := make([]output.FrontendRouteEntry, 0, len(mounts))
	for _, mount := range mounts {
		entries = append(entries, mountEntry(mount))
	}
	return entries
}

func mountEntry(mount clifrontend.Mount) output.FrontendRouteEntry {
	entry := output.FrontendRouteEntry{
		ID:         mount.Id.String(),
		PathPrefix: mount.PathPrefix,
		Frontend:   mount.TargetFrontendId.String(),
	}
	if mount.Target != nil {
		entry.Frontend = mount.Target.Name
	}
	return entry
}

func resultEntry(route clifrontend.RouteResult) output.FrontendRouteEntry {
	if route.Frontend != nil {
		return mountEntry(*route.Frontend)
	}
	return routeEntry(*route.Function)
}
