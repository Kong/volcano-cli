package functions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	logsTypeBuild   = "build"
	logsTypeRuntime = "runtime"
	defaultLogLimit = 100
)

type logsOptions struct {
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
		Short: "Show function build or runtime logs",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := ""
			if len(args) > 1 {
				deploymentID = strings.TrimSpace(args[1])
			}
			return runLogs(cmd.Context(), logsOptions{
				deps:         deps,
				identifier:   strings.TrimSpace(args[0]),
				deploymentID: deploymentID,
				logsType:     logsType,
				limit:        limit,
				out:          cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "l", defaultLogLimit, "Maximum logs per API page")
	cmd.Flags().StringVar(&logsType, "type", "", "Log type to fetch: build or runtime")
	if err := cmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}
	return cmd
}

func runLogs(ctx context.Context, opts logsOptions) error {
	logsType, err := normalizeLogsType(opts.logsType)
	if err != nil {
		return err
	}
	if logsType == logsTypeRuntime && opts.deploymentID != "" {
		return errors.New("deployment-id is only supported with --type build")
	}

	service := clifunction.NewService(opts.deps)
	function, err := service.Resolve(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if logsType == logsTypeRuntime {
		fmt.Fprintf(opts.out, "Fetching runtime logs for function %s\n\n", function.Name)
		return output.PrintLogs(opts.out, func(nextToken string) (*apiclient.GetLogsResponse, error) {
			return service.RuntimeLogs(ctx, function.Id, opts.limit, nextToken)
		})
	}

	deploymentID := function.CurrentDeploymentId
	if opts.deploymentID == "" {
		if deploymentID == nil {
			fmt.Fprintln(opts.out, "No deployments found for this function")
			return nil
		}
	} else {
		deployment, err := service.ResolveDeployment(ctx, function.Id, opts.deploymentID)
		if errors.Is(err, api.ErrNotFound) {
			return fmt.Errorf("deployment %q not found for function %s", opts.deploymentID, function.Name)
		}
		if err != nil {
			return err
		}
		deploymentID = &deployment.Id
	}

	fmt.Fprintf(opts.out, "Fetching build logs for function %s deployment %s\n\n", function.Name, deploymentID.String())
	return output.PrintLogs(opts.out, func(nextToken string) (*apiclient.GetLogsResponse, error) {
		return service.DeploymentLogs(ctx, function.Id, *deploymentID, opts.limit, nextToken)
	})
}

func normalizeLogsType(value string) (string, error) {
	logsType := strings.ToLower(strings.TrimSpace(value))
	switch logsType {
	case logsTypeBuild, logsTypeRuntime:
		return logsType, nil
	default:
		return "", errors.New("--type must be one of: build, runtime")
	}
}
