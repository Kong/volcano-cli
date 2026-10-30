package durable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	upgradecmd "github.com/Kong/volcano-cli/internal/cmd/upgrade"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/logfollow"
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
	follow       bool
	out          io.Writer
	printNotices func()
}

func newLogs(deps cliruntime.Deps) *cobra.Command {
	var limit int
	var logsType string
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <name-or-id> [deployment-id]",
		Short: "Show a durable function's build or runtime logs",
		Long: `Read the logs of one durable function.

--type build is the deploy's own output, and is where a deploy that ended in
"failed" says why. It reads the deployment the function is on, which after a
failed deploy is that deploy, or the one named by deployment-id.

--type runtime is what the function logged while its executions ran, across
every execution. The code between context operations runs again on each resume,
so a line logged there appears once per resume.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "durable logs order-pipeline --type build"),
			cliruntime.CommandPath(deps, "durable logs order-pipeline 4f1c2b8e-3c2a-4b7e-9a51-6d0e2f7a9c13 --type build"),
			cliruntime.CommandPath(deps, "durable logs order-pipeline --type runtime --follow")),
		Args: cobra.RangeArgs(1, 2),
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
				follow:       follow,
				out:          cmd.OutOrStdout(),
				printNotices: func() { upgradecmd.PrintAPIInstructionNotices(cmd, deps) },
			})
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "l", defaultLogLimit, "Maximum logs per API page")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream logs as new events arrive")
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

	service := clidurable.NewService(opts.deps)
	function, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if logsType == logsTypeRuntime {
		return runtimeLogs(ctx, opts, service, function)
	}
	if opts.deploymentID == "" {
		if function.CurrentDeploymentId == nil {
			fmt.Fprintln(opts.out, "No deployments found for this durable function")
			return nil
		}
		return buildLogs(ctx, opts, service, function.Id, *function.CurrentDeploymentId, function.Name)
	}

	deployment, err := service.ResolveDeployment(ctx, function.Id, opts.deploymentID)
	if errors.Is(err, api.ErrNotFound) {
		return fmt.Errorf("deployment %q %w for durable function %s", opts.deploymentID, api.ErrNotFound, function.Name)
	}
	if err != nil {
		return err
	}
	// Following stops when the function leaves provisioning, which says
	// nothing about any deployment but the one it is on.
	if opts.follow && (function.CurrentDeploymentId == nil || *function.CurrentDeploymentId != deployment.Id) {
		return errors.New("--follow reads only the deployment the function is on; drop --follow to read an earlier one")
	}
	return buildLogs(ctx, opts, service, function.Id, deployment.Id, function.Name)
}

func runtimeLogs(
	ctx context.Context, opts logsOptions, service clidurable.Service, function *apiclient.DurableFunction,
) error {
	if opts.follow {
		fmt.Fprintf(opts.out, "Following runtime logs for durable function %s\n\n", function.Name)
		return logfollow.RuntimeWithStreamOpened(ctx, opts.deps, opts.out,
			func(ctx context.Context, lastEventID string) (*api.ProjectLogStream, error) {
				return service.StreamRuntimeLogs(ctx, function.Id, opts.limit, lastEventID)
			}, opts.printNotices)
	}
	fmt.Fprintf(opts.out, "Fetching runtime logs for durable function %s\n\n", function.Name)
	return output.PrintSearchLogs(opts.out, func(cursor string) (*apiclient.LogSearchResponse, error) {
		return service.RuntimeLogs(ctx, function.Id, opts.limit, cursor)
	})
}

func buildLogs(
	ctx context.Context,
	opts logsOptions,
	service clidurable.Service,
	functionID, deploymentID uuid.UUID,
	name string,
) error {
	if opts.follow {
		fmt.Fprintf(opts.out, "Following build logs for durable function %s deployment %s\n\n",
			name, deploymentID.String())
		return followBuildLogs(ctx, opts, service, functionID, deploymentID)
	}
	fmt.Fprintf(opts.out, "Fetching build logs for durable function %s deployment %s\n\n",
		name, deploymentID.String())
	return output.PrintSearchLogs(opts.out, func(cursor string) (*apiclient.LogSearchResponse, error) {
		return service.DeploymentLogs(ctx, functionID, deploymentID, opts.limit, cursor)
	})
}

// followBuildLogs streams until the deploy settles. The durable collection has
// no deployment read, so the function's own status is what says the deploy is
// over — it leaves provisioning exactly when the deployment it is on finishes.
func followBuildLogs(
	ctx context.Context,
	opts logsOptions,
	service clidurable.Service,
	functionID, deploymentID uuid.UUID,
) error {
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := service.StreamDeploymentLogs(streamCtx, functionID, deploymentID, opts.limit)
	if err != nil {
		cancel()
		return err
	}
	if opts.printNotices != nil {
		opts.printNotices()
	}
	return logfollow.Deployment(ctx, opts.deps, opts.out, stream, cancel, func(ctx context.Context) (bool, error) {
		function, err := service.Get(ctx, functionID.String())
		if err != nil {
			return false, err
		}
		return function.Status != apiclient.DurableFunctionStatusProvisioning, nil
	}, func(ctx context.Context, printed map[string]struct{}) error {
		return output.PrintSearchLogsSkipping(opts.out, func(cursor string) (*apiclient.LogSearchResponse, error) {
			return service.DeploymentLogs(ctx, functionID, deploymentID, opts.limit, cursor)
		}, printed)
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
