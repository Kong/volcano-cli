package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// maxPreviewTTL is the longest a preview may live, which the API enforces.
const maxPreviewTTL = 30 * 24 * time.Hour

func newPreview(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "preview",
		Aliases: []string{"previews"},
		Short:   "Manage function previews",
		Long: `Deploy a private copy of a function to its own Lambda and URL, check it, and promote it.

A preview builds the function's source from your project directory, runs with
the function's current project variables, and is invoked at its own URL with
your project credentials. It has none of the function's schedules or warmers.
Promoting a preview deploys its exact image to the function without
rebuilding, and 'functions rollback' undoes that.`,
	}
	cmd.AddCommand(newPreviewCreate(deps), newPreviewList(deps), newPreviewGet(deps),
		newPreviewDeploy(deps), newPreviewDelete(deps), newPreviewPromote(deps))
	return cmd
}

func newServing(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "serving <function>",
		Short: "Show what a function runs",
		Long:  "Show whether a function runs its own build or a promoted preview's, and its latest promotion or rollback.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifunction.NewService(deps)
			function, serving, err := service.Serving(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			printServing(cmd.Context(), cmd.OutOrStdout(), service, function, serving)
			return nil
		},
	}
}

func newRollback(deps cliruntime.Deps) *cobra.Command {
	var toProduction bool
	cmd := &cobra.Command{
		Use:   "rollback <function>",
		Short: "Roll a function back to the build it ran before",
		Long: `Deploy the build a function ran before its last promotion or rollback, without rebuilding.

A second rollback undoes the first. With --to-production, return to the
function's own newest build instead. While previews are unavailable, a rollback
that would run a preview's build again is refused; --to-production still works.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifunction.NewService(deps)
			function, serving, err := service.RollbackServing(cmd.Context(), strings.TrimSpace(args[0]), toProduction)
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Function '%s' rollback is deploying", function.Name)
			printServing(cmd.Context(), cmd.OutOrStdout(), service, function, serving)
			return nil
		},
	}
	cmd.Flags().BoolVar(&toProduction, "to-production", false, "Return to the function's own build")
	return cmd
}

func newPreviewCreate(deps cliruntime.Deps) *cobra.Command {
	var (
		name string
		path string
		ttl  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "create <function> --name <name>",
		Short: "Deploy a new preview of a function",
		Long: `Package the function's source from a project directory and deploy it as a new preview.

The source is found as 'functions deploy' finds it, under volcano/functions in
the project directory. The preview's first deployment starts and its URL is
shown by 'preview get' once it is ready.`,
		Example: "  " + cliruntime.CommandPath(deps, "functions preview create api --name checkout-v2 --ttl 72h"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := previewTTLSeconds(ttl)
			if err != nil {
				return err
			}
			service := clifunction.NewService(deps)
			source, err := packageFunctionPreview(cmd.Context(), cmd.OutOrStdout(), service, strings.TrimSpace(args[0]), path)
			if err != nil {
				return err
			}
			created, err := service.CreateCandidate(cmd.Context(), clifunction.CandidateRequest{
				Function: strings.TrimSpace(args[0]), Name: name, TTLSeconds: seconds, Source: source,
			})
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Preview '%s' deployment started", created.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Run '%s' for its URL once it is ready.\n",
				cliruntime.CommandPath(deps, "functions preview get "+strings.TrimSpace(args[0])+" "+created.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Preview name, unique within the function (required)")
	cmd.Flags().StringVar(&path, "path", ".", "Path to the project directory")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "Delete the preview after this long, at most 720h (default: keep until deleted)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// previewTTLSeconds checks --ttl. Under a second would truncate to 0, which
// keeps the preview until deleted: the opposite of a short TTL.
func previewTTLSeconds(ttl time.Duration) (int, error) {
	if ttl < 0 || ttl > maxPreviewTTL {
		return 0, fmt.Errorf("--ttl must be between 0 and %s", maxPreviewTTL)
	}
	if ttl > 0 && ttl < time.Second {
		return 0, errors.New("--ttl must be at least 1s, or 0 to keep the preview until it is deleted")
	}
	return int(ttl / time.Second), nil
}

func newPreviewList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <function>",
		Short: "List a function's previews",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, previews, err := clifunction.NewService(deps).ListCandidates(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if len(previews) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No previews.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tSERVING\tURL\tEXPIRES")
			for i := range previews {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", previews[i].Name, previews[i].Status, servingLabel(&previews[i]),
					valueOr(previews[i].Url, "-"), expiry(previews[i].ExpiresAt))
			}
			return w.Flush()
		},
	}
}

func newPreviewGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <function> <preview>",
		Short: "Show a preview",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, err := clifunction.NewService(deps).GetCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			printPreview(cmd.OutOrStdout(), preview)
			return nil
		},
	}
}

func newPreviewDeploy(deps cliruntime.Deps) *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "deploy <function> <preview>",
		Short: "Deploy a new build to an existing preview",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifunction.NewService(deps)
			source, err := packageFunctionPreview(cmd.Context(), cmd.OutOrStdout(), service, strings.TrimSpace(args[0]), path)
			if err != nil {
				return err
			}
			preview, deployment, err := service.DeployCandidate(cmd.Context(), clifunction.CandidateDeploy{
				Function: strings.TrimSpace(args[0]), Candidate: strings.TrimSpace(args[1]), Source: source,
			})
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Preview '%s' deployment started", preview.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Deployment: %s\n", deployment.Id)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "Path to the project directory")
	return cmd
}

func newPreviewDelete(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <function> <preview>",
		Short: "Delete a preview",
		Long: `Delete a preview and remove its Lambda, URL and stored source.

A preview the function runs cannot be deleted: roll back first. Deleting a
preview kept only for 'functions rollback' gives that rollback up.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, err := clifunction.NewService(deps).DeleteCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Preview '%s' is being deleted", preview.Name)
			return nil
		},
	}
}

func newPreviewPromote(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "promote <function> <preview>",
		Short: "Deploy a preview's exact image to the function",
		Long: `Deploy a preview's built image to the function, without rebuilding.

The preview must be ready, built with the function's current variables, runtime
and handler, and in every region the function deploys to. The function keeps
its URL, visibility, schedules and warmers. The promotion runs as a deployment
of the function; 'functions serving' shows when it is applied.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, serving, err := clifunction.NewService(deps).PromoteCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Promoting preview '%s'", preview.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Revision: %d\n", serving.Revision)
			return nil
		},
	}
}

// packageFunctionPreview packages the function's source as 'functions deploy'
// does, found by the function's name under the project directory.
func packageFunctionPreview(ctx context.Context, out io.Writer, service clifunction.Service, identifier, rawPath string) ([]byte, error) {
	function, err := service.Get(ctx, identifier)
	if err != nil {
		return nil, err
	}
	baseDir, err := filepath.Abs(valueOrDefault(strings.TrimSpace(rawPath), "."))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project path: %w", err)
	}
	source, err := functionSource(ctx, service, baseDir, function.Name)
	if err != nil {
		return nil, err
	}
	pkg, err := clifunction.PackageSource(source, baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to package function %s: %w", function.Name, err)
	}
	fmt.Fprintf(out, "Packaged %s (%s)\n", source.Path, archive.FormatSize(pkg.Size))
	return pkg.ArchiveData, nil
}

func functionSource(ctx context.Context, service clifunction.Service, baseDir, name string) (clifunction.SourceInfo, error) {
	catalog, err := service.RuntimeCatalog(ctx)
	if err != nil {
		return clifunction.SourceInfo{}, err
	}
	sources, err := clifunction.ScanSources(baseDir, catalog)
	if err != nil {
		return clifunction.SourceInfo{}, fmt.Errorf("failed to scan functions: %w", err)
	}
	for _, source := range sources {
		if clifunction.MatchesTarget(source, name, baseDir) {
			return source, nil
		}
	}
	return clifunction.SourceInfo{}, fmt.Errorf("no source for function %q under %s", name, filepath.Join(baseDir, "volcano", "functions"))
}

func printPreview(out io.Writer, preview *apiclient.FunctionCandidate) {
	fmt.Fprintf(out, "Name:      %s\n", preview.Name)
	fmt.Fprintf(out, "ID:        %s\n", preview.Id)
	fmt.Fprintf(out, "Status:    %s\n", preview.Status)
	fmt.Fprintf(out, "URL:       %s\n", valueOr(preview.Url, "-"))
	fmt.Fprintf(out, "Expires:   %s\n", expiry(preview.ExpiresAt))
	fmt.Fprintf(out, "Serving:   %s\n", servingLabel(preview))
	if preview.ConfigurationStatus != nil {
		fmt.Fprintf(out, "Variables: %s\n", *preview.ConfigurationStatus)
	}
	if deployment := preview.LatestDeployment; deployment != nil {
		fmt.Fprintf(out, "Deployment: %s (%s)\n", deployment.Id, deployment.Status)
		if deployment.ImageUri != nil {
			fmt.Fprintf(out, "Image:     %s\n", *deployment.ImageUri)
		}
		if deployment.ErrorMessage != nil && *deployment.ErrorMessage != "" {
			fmt.Fprintf(out, "Error:     %s\n", *deployment.ErrorMessage)
		}
	}
}

func servingLabel(preview *apiclient.FunctionCandidate) string {
	switch {
	case preview.Serving:
		return "yes"
	case preview.KeptForRollback:
		return "rollback"
	default:
		return "no"
	}
}

func printServing(ctx context.Context, out io.Writer, service clifunction.Service, function *apiclient.Function, serving *apiclient.FunctionServing) {
	current := "its own build"
	if serving.CandidateId != nil {
		current = "preview '" + nameOrID(ctx, service, function, serving.CandidateId) + "'"
	}
	fmt.Fprintf(out, "Function '%s' runs %s (revision %d)\n", function.Name, current, serving.Revision)
	if serving.PreviousCandidateId != nil {
		fmt.Fprintf(out, "A rollback returns to preview '%s'\n", nameOrID(ctx, service, function, serving.PreviousCandidateId))
	}
	if activation := serving.Activation; activation != nil {
		fmt.Fprintf(out, "Latest %s: %s (deployment %s)\n", activation.Kind, activation.Status, activation.DeploymentId)
		fmt.Fprintf(out, "Image: %s\n", activation.ImageUri)
		if activation.ErrorMessage != nil && *activation.ErrorMessage != "" {
			fmt.Fprintf(out, "Error: %s\n", *activation.ErrorMessage)
		}
	}
}

// nameOrID names a preview, or shows its ID when the name cannot be read.
func nameOrID(ctx context.Context, service clifunction.Service, function *apiclient.Function, id *uuid.UUID) string {
	name, _ := service.CandidateName(ctx, function, id)
	if name == "" {
		return id.String()
	}
	return name
}

func valueOr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func expiry(at *time.Time) string {
	if at == nil {
		return "never"
	}
	return at.Local().Format(time.RFC3339)
}
