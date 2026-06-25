package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	frontendLogsTypeBuild   = "build"
	frontendLogsTypeRuntime = "runtime"
	defaultFrontendLogLimit = 100
)

type frontendLogsOptions struct {
	deps         cliruntime.Deps
	identifier   string
	deploymentID string
	logsType     string
	limit        int
	out          io.Writer
}

func newLogs(deps cliruntime.Deps) *cobra.Command {
	var limit int
	var logsType string
	cmd := &cobra.Command{
		Use:   "logs <name-or-id> [deployment-id]",
		Short: "Show frontend build or runtime logs",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := ""
			if len(args) > 1 {
				deploymentID = strings.TrimSpace(args[1])
			}
			return runLogs(cmd.Context(), frontendLogsOptions{
				deps:         deps,
				identifier:   strings.TrimSpace(args[0]),
				deploymentID: deploymentID,
				logsType:     logsType,
				limit:        limit,
				out:          cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "l", defaultFrontendLogLimit, "Maximum logs per API page")
	cmd.Flags().StringVar(&logsType, "type", "", "Log type to fetch: build or runtime")
	if err := cmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}
	return cmd
}

func runLogs(ctx context.Context, opts frontendLogsOptions) error {
	logsType, err := normalizeLogsType(opts.logsType)
	if err != nil {
		return err
	}
	if logsType == frontendLogsTypeRuntime && opts.deploymentID != "" {
		return errors.New("deployment-id is only supported with --type build")
	}

	service := clifrontend.NewService(opts.deps)
	frontend, err := service.Resolve(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if logsType == frontendLogsTypeRuntime {
		fmt.Fprintf(opts.out, "Fetching runtime logs for frontend %s\n\n", frontend.Name)
		return output.PrintSearchLogs(opts.out, func(cursor string) (*apiclient.LogSearchResponse, error) {
			return service.RuntimeLogs(ctx, frontend.Id, opts.limit, cursor)
		})
	}

	deploymentID := frontend.CurrentDeploymentId
	if opts.deploymentID == "" {
		if deploymentID == nil {
			deployment, err := service.LatestDeployment(ctx, frontend.Id)
			if err != nil && !errors.Is(err, api.ErrNotFound) {
				return err
			}
			if deployment != nil {
				deploymentID = &deployment.Id
			}
		}
		if deploymentID == nil {
			fmt.Fprintln(opts.out, "No deployments found for this frontend")
			return nil
		}
	} else {
		deployment, err := service.ResolveDeployment(ctx, frontend.Id, opts.deploymentID)
		if errors.Is(err, api.ErrNotFound) {
			return fmt.Errorf("deployment %q not found for frontend %s", opts.deploymentID, frontend.Name)
		}
		if err != nil {
			return err
		}
		deploymentID = &deployment.Id
	}

	fmt.Fprintf(opts.out, "Fetching build logs for frontend %s deployment %s\n\n", frontend.Name, deploymentID.String())
	return output.PrintSearchLogs(opts.out, func(cursor string) (*apiclient.LogSearchResponse, error) {
		return service.DeploymentLogs(ctx, frontend.Id, *deploymentID, opts.limit, cursor)
	})
}

func normalizeLogsType(value string) (string, error) {
	logsType := strings.ToLower(strings.TrimSpace(value))
	switch logsType {
	case frontendLogsTypeBuild, frontendLogsTypeRuntime:
		return logsType, nil
	default:
		return "", errors.New("--type must be one of: build, runtime")
	}
}
