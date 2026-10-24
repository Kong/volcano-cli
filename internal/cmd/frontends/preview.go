package frontends

import (
	"context"
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
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// maxPreviewTTL is the longest a preview may live, which the API enforces.
const maxPreviewTTL = 30 * 24 * time.Hour

func newPreview(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "preview",
		Aliases: []string{"previews"},
		Short:   "Manage frontend previews",
		Long: `Deploy a private copy of a frontend to its own URL, check it, and promote it.

A preview builds from a local directory and is served at its own host, not at
the frontend's. It runs with the frontend's current project variables, and
anyone who holds its URL can open it: the URL is not indexed, but it is not
private. Promoting a preview points the frontend's own traffic at it without
rebuilding, and 'frontends rollback' undoes that.`,
	}
	cmd.AddCommand(newPreviewCreate(deps))
	cmd.AddCommand(newPreviewList(deps))
	cmd.AddCommand(newPreviewGet(deps))
	cmd.AddCommand(newPreviewDeploy(deps))
	cmd.AddCommand(newPreviewDelete(deps))
	cmd.AddCommand(newPreviewPromote(deps))
	return cmd
}

func newServing(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "serving <frontend>",
		Short: "Show what a frontend serves",
		Long:  "Show whether a frontend serves its own build or a promoted preview, and the preview a rollback returns to.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifrontend.NewService(deps)
			frontend, serving, err := service.Serving(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			printServing(cmd.Context(), cmd.OutOrStdout(), service, frontend, serving)
			return nil
		},
	}
}

func newRollback(deps cliruntime.Deps) *cobra.Command {
	var toProduction bool
	cmd := &cobra.Command{
		Use:   "rollback <frontend>",
		Short: "Roll a frontend back to what it served before",
		Long: `Return a frontend's traffic to the build it served before the last promotion.

With --to-production, return to the frontend's own build instead, whatever it
served before. A rollback is itself undone by promoting again.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifrontend.NewService(deps)
			frontend, serving, err := service.RollbackServing(cmd.Context(), strings.TrimSpace(args[0]), toProduction)
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Frontend '%s' rolled back", frontend.Name)
			printServing(cmd.Context(), cmd.OutOrStdout(), service, frontend, serving)
			return nil
		},
	}
	cmd.Flags().BoolVar(&toProduction, "to-production", false, "Return to the frontend's own build")
	return cmd
}

func newPreviewCreate(deps cliruntime.Deps) *cobra.Command {
	var (
		name string
		path string
		ttl  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "create <frontend> --name <name>",
		Short: "Deploy a new preview of a frontend",
		Long: `Package a local project directory and deploy it as a new preview of a frontend.

The preview's first deployment starts and its URL is shown by 'preview get'
once it is ready.`,
		Example: "  " + cliruntime.CommandPath(deps, "frontends preview create web --name redesign --path ./apps/web --ttl 72h"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if ttl < 0 || ttl > maxPreviewTTL {
				return fmt.Errorf("--ttl must be between 0 and %s", maxPreviewTTL)
			}
			packaged, err := packagePreview(cmd.OutOrStdout(), path)
			if err != nil {
				return err
			}
			created, err := clifrontend.NewService(deps).CreateCandidate(cmd.Context(), clifrontend.CandidateRequest{
				Frontend: strings.TrimSpace(args[0]), Name: name,
				TTLSeconds: int(ttl / time.Second), Archive: packaged,
			})
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Preview '%s' deployment started", created.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Run '%s' for its URL once it is ready.\n",
				cliruntime.CommandPath(deps, "frontends preview get "+strings.TrimSpace(args[0])+" "+created.Name))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Preview name, unique within the frontend (required)")
	cmd.Flags().StringVar(&path, "path", ".", "Path to the frontend project directory")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "Delete the preview after this long, at most 720h (default: keep until deleted)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newPreviewList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <frontend>",
		Short: "List a frontend's previews",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, previews, err := clifrontend.NewService(deps).ListCandidates(cmd.Context(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			if len(previews) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No previews.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tURL\tEXPIRES")
			for i := range previews {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", previews[i].Name, previews[i].Status,
					valueOr(previews[i].Url, "-"), expiry(previews[i].ExpiresAt))
			}
			return w.Flush()
		},
	}
}

func newPreviewGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <frontend> <preview>",
		Short: "Show a preview",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, err := clifrontend.NewService(deps).GetCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
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
		Use:   "deploy <frontend> <preview>",
		Short: "Deploy a new build to an existing preview",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			packaged, err := packagePreview(cmd.OutOrStdout(), path)
			if err != nil {
				return err
			}
			preview, deployment, err := clifrontend.NewService(deps).DeployCandidate(cmd.Context(), clifrontend.CandidateDeploy{
				Frontend: strings.TrimSpace(args[0]), Candidate: strings.TrimSpace(args[1]), Archive: packaged,
			})
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Preview '%s' deployment started", preview.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Deployment: %s\n", deployment.Id)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "Path to the frontend project directory")
	return cmd
}

func newPreviewDelete(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <frontend> <preview>",
		Short: "Delete a preview",
		Long: `Delete a preview and remove its runtime.

A preview the frontend serves, or that a rollback returns to, cannot be deleted:
roll back first.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			preview, err := clifrontend.NewService(deps).DeleteCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
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
		Use:   "promote <frontend> <preview>",
		Short: "Make a preview what the frontend serves",
		Long: `Point the frontend's own traffic at a preview, without rebuilding.

The preview must be ready and have been built with the frontend's current
project variables. The frontend's custom domains, metering and rate limits stay
the frontend's. While a preview is promoted, deploying the frontend itself is
refused; run 'frontends rollback' first.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := clifrontend.NewService(deps)
			preview, serving, err := service.PromoteCandidate(cmd.Context(), strings.TrimSpace(args[0]), strings.TrimSpace(args[1]))
			if err != nil {
				return err
			}
			output.Success(cmd.OutOrStdout(), "Frontend now serves preview '%s'", preview.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Revision: %d\n", serving.Revision)
			return nil
		},
	}
}

// packagePreview packages a project directory as the frontend deploy does,
// leaving the app root to the frontend's own.
func packagePreview(out io.Writer, rawPath string) ([]byte, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	absPath, err := filepath.Abs(rawPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve frontend path: %w", err)
	}
	fmt.Fprintf(out, "Packaging frontend directory: %s\n", absPath)
	pkg, err := clifrontend.PackageDirectory(absPath, clifrontend.PackageOptions{})
	if err != nil {
		return nil, err
	}
	if pkg.PackagingRoot != absPath {
		fmt.Fprintf(out, "Packaging workspace root: %s\n", pkg.PackagingRoot)
	}
	for _, link := range pkg.SkippedSymlinks {
		fmt.Fprintf(out, "Warning: skipped symlink %s (symlinks are not archived)\n", link)
	}
	fmt.Fprintf(out, "Archive size: %s\n", archive.FormatSize(pkg.Size))
	fmt.Fprintln(out, "Uploading archive...")
	return pkg.Archive, nil
}

func printPreview(out io.Writer, preview *apiclient.FrontendCandidate) {
	fmt.Fprintf(out, "Name:     %s\n", preview.Name)
	fmt.Fprintf(out, "ID:       %s\n", preview.Id)
	fmt.Fprintf(out, "Status:   %s\n", preview.Status)
	fmt.Fprintf(out, "URL:      %s\n", valueOr(preview.Url, "-"))
	fmt.Fprintf(out, "Expires:  %s\n", expiry(preview.ExpiresAt))
	if preview.ConfigurationStatus != nil {
		fmt.Fprintf(out, "Variables: %s\n", *preview.ConfigurationStatus)
	}
	if preview.Deployment != nil {
		fmt.Fprintf(out, "Deployment: %s (%s)\n", preview.Deployment.Id, preview.Deployment.Status)
		if preview.Deployment.ErrorMessage != nil && *preview.Deployment.ErrorMessage != "" {
			fmt.Fprintf(out, "Error:    %s\n", *preview.Deployment.ErrorMessage)
		}
	}
}

func printServing(ctx context.Context, out io.Writer, service clifrontend.Service, frontend *apiclient.Frontend, serving *apiclient.FrontendServing) {
	current := "its own build"
	if serving.CandidateId != nil {
		current = "preview '" + nameOrID(ctx, service, frontend, serving.CandidateId) + "'"
	}
	fmt.Fprintf(out, "Frontend '%s' serves %s (revision %d)\n", frontend.Name, current, serving.Revision)
	if serving.PreviousCandidateId != nil {
		fmt.Fprintf(out, "A rollback returns to preview '%s'\n", nameOrID(ctx, service, frontend, serving.PreviousCandidateId))
	}
}

// nameOrID names a preview, or shows its ID when the name cannot be read.
func nameOrID(ctx context.Context, service clifrontend.Service, frontend *apiclient.Frontend, id *uuid.UUID) string {
	name, _ := service.CandidateName(ctx, frontend, id)
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

func expiry(at *time.Time) string {
	if at == nil {
		return "never"
	}
	return at.Local().Format(time.RFC3339)
}
