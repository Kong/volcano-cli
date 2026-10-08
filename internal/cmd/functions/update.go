package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
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
		Long: fmt.Sprintf(`Update function settings.

--visibility sets who can invoke the function:
  private        Service keys and schedulers only. New functions start here.
  authenticated  Also your project's signed-in users, including anonymous
                 sign-ins.
  public         Also anon keys with functions.invoke, and frontend routes.

--public is the same as --visibility public.

A private function answers every other caller with the same 404 as a missing
function. An anon key invoking an authenticated function by ID gets 403; by
name, as the SDK invokes, it gets 404. If your app gets 404 for a function you
deployed, check its visibility with %s.

A durable function's visibility is set through config deploy or durable deploy.`,
			cliruntime.CommandPath(deps, "functions get NAME")),
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
	var durable *clifunction.DurableFunctionError
	var routed *clifunction.RoutedFunctionError
	switch {
	case errors.As(err, &durable):
		return fmt.Errorf("%w: declare visibility: %s under it in volcano-config.yaml and run %s, or redeploy it with %s",
			err, visibility, cliruntime.CommandPath(opts.deps, "config deploy"),
			cliruntime.CommandPath(opts.deps, "durable deploy -f "+durable.Name+" --visibility "+string(visibility)))
	case errors.As(err, &routed):
		return fmt.Errorf("%w\n%s", err, routedFunctionHint(opts.deps, routed))
	case api.Status(err) == http.StatusBadRequest:
		return fmt.Errorf("%w\n%s", err, cmdutil.VisibilityLevelsHint(opts.deps))
	case err != nil:
		return err
	}
	if updated.Visibility == "" {
		return fmt.Errorf("%s: check who can invoke '%s' with %s",
			cmdutil.VisibilityLevelsUnsupported, updated.Name, cliruntime.CommandPath(opts.deps, "functions get "+updated.Name))
	}
	output.Success(opts.out, "Function '%s' visibility set to %s", updated.Name, updated.Visibility)
	return nil
}

// routedFunctionHint names the routes that keep a function public. The CLI
// manages routes only in the cloud; a local project's routes come from its
// manifest.
func routedFunctionHint(deps cliruntime.Deps, routed *clifunction.RoutedFunctionError) string {
	var hint strings.Builder
	if deps.LocalMode {
		hint.WriteString("frontend routes forward to it; remove them from function_routes in volcano-config.yaml and run " +
			cliruntime.CommandPath(deps, "config deploy") + ":")
		for _, route := range routed.Routes {
			fmt.Fprintf(&hint, "\n  %s %s", route.Frontend, route.PathPrefix)
		}
		return hint.String()
	}
	hint.WriteString("frontend routes forward to it; remove them first:")
	for _, route := range routed.Routes {
		fmt.Fprintf(&hint, "\n  %s", cliruntime.CommandPath(deps, "frontends routes delete "+route.Frontend+" "+route.PathPrefix))
	}
	return hint.String()
}
