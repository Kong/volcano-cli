package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
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
		Short: "Manage frontend function routes",
		Long: `Manage the paths of a frontend that forward requests to functions.

A route sends every request under a path prefix, such as /api/session, to an
HTTP-mode function, so the browser calls it on the frontend's own origin. The
function must be public: anyone who can load the frontend can call it, so it
has to authenticate its callers itself. The longest matching prefix wins.`,
	}
	cmd.AddCommand(newRoutesList(deps))
	cmd.AddCommand(newRoutesCreate(deps))
	cmd.AddCommand(newRoutesUpdate(deps))
	cmd.AddCommand(newRoutesDelete(deps))
	return cmd
}

// NewRoutesCloudOnly stands in for routes under the deprecated root
// "volcano frontends" alias. The root is where local commands live, and the
// alias would otherwise send a local project's route changes to the cloud.
// Local development has no frontends commands, so routes there come only from
// volcano-config.yaml. Flag parsing is off so any flags reach the refusal.
func NewRoutesCloudOnly() *cobra.Command {
	return &cobra.Command{
		Use:   "routes",
		Short: "Manage frontend function routes",
		Long: `Manage the paths of a frontend that forward requests to functions.

This is a cloud command: run 'volcano cloud frontends routes ...'. Local
development has no frontends commands; declare function_routes in
volcano-config.yaml and run 'volcano config deploy' instead.`,
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag parsing is off, so cobra does not answer --help itself.
			if slices.ContainsFunc(args, func(arg string) bool { return arg == "--help" || arg == "-h" }) {
				return cmd.Help()
			}
			return errors.New(`"frontends routes" is a cloud command: run 'volcano cloud frontends routes ...'. ` +
				"Local development has no frontends commands; declare function_routes in volcano-config.yaml " +
				"and run 'volcano config deploy' instead")
		},
	}
}

func newRoutesList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <frontend>",
		Short: "List a frontend's function routes",
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
		Short: "Forward a frontend path to a function",
		Long: `Forward every request under a path prefix of a frontend to a function.

The function must be a public, HTTP-mode standard function in the same project.
With --strip-prefix the function sees the rest of the path, or / for the prefix
itself; without it, the function sees the full path.`,
		Example: "  " + cliruntime.CommandPath(deps, "frontends routes create web --path /api/session --function session --strip-prefix"),
		Args:    cobra.ExactArgs(1),
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
	cmd.Flags().BoolVar(&input.StripPrefix, "strip-prefix", false, "Remove the prefix from the path the function sees")
	if err := cmd.MarkFlagRequired("path"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("function"); err != nil {
		panic(err)
	}
	return cmd
}

func newRoutesUpdate(deps cliruntime.Deps) *cobra.Command {
	var pathPrefix, function string
	var stripPrefix bool
	cmd := &cobra.Command{
		Use:   "update <frontend> <path-or-id>",
		Short: "Change a frontend function route",
		Long: `Change a route's path prefix, target function, or prefix stripping.

Name the route by its current path prefix or its ID. Flags you leave out keep
their current value.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends routes update web /api/session --function session-v2"),
			cliruntime.CommandPath(deps, "frontends routes update web /api/session --strip-prefix=false")),
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
			if cmd.Flags().Changed("strip-prefix") {
				opts.update.StripPrefix = &stripPrefix
			}
			return runRoutesUpdate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&pathPrefix, "path", "", "New path prefix")
	cmd.Flags().StringVar(&function, "function", "", "Function name or ID to forward to")
	cmd.Flags().BoolVar(&stripPrefix, "strip-prefix", false, "Remove the prefix from the path the function sees")
	return cmd
}

func newRoutesDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <frontend> <path-or-id>",
		Short: "Stop forwarding a frontend path to a function",
		Long: `Delete a frontend function route, named by its path prefix or ID.

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
	output.FrontendRoutes(opts.out, frontend.Name, routeEntries(routes))
	return nil
}

func runRoutesCreate(ctx context.Context, opts routesCreateOptions) error {
	if err := validatePathPrefix(opts.input.PathPrefix); err != nil {
		return err
	}
	frontend, route, err := clifrontend.NewService(opts.deps).CreateRoute(ctx, opts.frontend, opts.input)
	if err != nil {
		return withTargetHint(opts.deps, err)
	}
	entry := routeEntry(route)
	output.Success(opts.out, "Route %s on frontend '%s' now forwards to function '%s'", entry.PathPrefix, frontend.Name, entry.Function)
	output.FrontendRoute(opts.out, entry)
	fmt.Fprintf(opts.out, "Anyone who can load '%s' can call '%s' under %s; the function must authenticate its own callers.\n",
		frontend.Name, entry.Function, entry.PathPrefix)
	return nil
}

func runRoutesUpdate(ctx context.Context, opts routesUpdateOptions) error {
	if opts.update.PathPrefix == nil && opts.update.Function == nil && opts.update.StripPrefix == nil {
		return errors.New("specify at least one of --path, --function, or --strip-prefix")
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
	entry := routeEntry(route)
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
		confirmed, err := confirm.Delete(opts.in, opts.out, "route", fmt.Sprintf("%s on frontend %s", route.PathPrefix, frontend.Name))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.DeleteRouteByID(ctx, frontend.Id, route.Id); err != nil {
		return err
	}
	output.Success(opts.out, "Route %s deleted from frontend '%s'", route.PathPrefix, frontend.Name)
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
