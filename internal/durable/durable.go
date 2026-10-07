// Package durable performs authenticated Volcano durable function workflows.
//
// Durable functions are their own collection: they are started rather than
// invoked, an execution outlives the request that began it, and the API
// resolves a function by name or id server-side. That last part is why this
// package is thinner than internal/function, which pages a list to turn a name
// into an id.
package durable

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Service performs durable function workflows against the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a durable function service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// ListPage returns one durable function page in the current project.
func (s Service) ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedDurableFunctions, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	functions, err := authenticated.API.ListDurableFunctions(ctx, authenticated.ProjectID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list durable functions: %w", err)
	}
	return functions, nil
}

// Deploy uploads one packaged source archive as a durable function. It creates
// the function on the first call for a name and redeploys it after that.
//
// isPublic is left nil to keep the deployed function's current visibility,
// which is what the collection does with an absent field. A new function
// starts private.
func (s Service) Deploy(ctx context.Context, pkg clifunction.Package, isPublic *bool) (*apiclient.DurableFunction, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	fn, err := authenticated.API.DeployDurableFunction(ctx, authenticated.ProjectID, api.DurableFunctionDeployInput{
		Name:          pkg.Name,
		Runtime:       pkg.Runtime,
		Handler:       pkg.Handler,
		SourceArchive: pkg.ArchiveData,
		IsPublic:      isPublic,
		VariableScope: pkg.VariableScope,
		Variables:     pkg.Variables,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to deploy durable function %q: %w", pkg.Name, err)
	}
	return fn, nil
}

// Get returns one durable function by name or id.
func (s Service) Get(ctx context.Context, identifier string) (*apiclient.DurableFunction, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	fn, err := authenticated.API.GetDurableFunction(ctx, authenticated.ProjectID, identifier)
	if err != nil {
		return nil, durableFunctionError("get", identifier, err)
	}
	return fn, nil
}

// Delete starts deleting one durable function by name or id.
func (s Service) Delete(ctx context.Context, identifier string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteDurableFunction(ctx, authenticated.ProjectID, identifier); err != nil {
		return durableFunctionError("delete", identifier, err)
	}
	return nil
}

// RuntimeLogs returns one runtime log search page for a durable function.
//
// The log routes are project-scoped and take a function id whatever collection
// it came from, so these four read a durable function's logs through the same
// endpoints a standard function's do.
func (s Service) RuntimeLogs(
	ctx context.Context, functionID uuid.UUID, limit int, cursor string,
) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFunctionLogs(ctx, authenticated.ProjectID, functionID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runtime logs: %w", err)
	}
	return logs, nil
}

// StreamRuntimeLogs opens a runtime log stream for a durable function, resuming
// after lastEventID when it is set.
func (s Service) StreamRuntimeLogs(
	ctx context.Context, functionID uuid.UUID, limit int, lastEventID string,
) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFunctionLogs(ctx, authenticated.ProjectID, functionID, limit, lastEventID)
	if err != nil {
		return nil, fmt.Errorf("failed to stream runtime logs: %w", err)
	}
	return stream, nil
}

// DeploymentLogs returns one build log search page for a durable function's
// deployment.
func (s Service) DeploymentLogs(
	ctx context.Context, functionID, deploymentID uuid.UUID, limit int, cursor string,
) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFunctionDeploymentLogs(
		ctx, authenticated.ProjectID, functionID, deploymentID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deployment logs: %w", err)
	}
	return logs, nil
}

// StreamDeploymentLogs opens a build log stream for a durable function's
// deployment.
func (s Service) StreamDeploymentLogs(
	ctx context.Context, functionID, deploymentID uuid.UUID, limit int,
) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFunctionDeploymentLogs(
		ctx, authenticated.ProjectID, functionID, deploymentID, limit, "")
	if err != nil {
		return nil, fmt.Errorf("failed to stream deployment logs: %w", err)
	}
	return stream, nil
}

// StartExecution starts one execution and returns its handle. Starting is
// asynchronous by construction, so this never carries a result.
func (s Service) StartExecution(
	ctx context.Context,
	identifier string,
	input api.DurableExecutionStartInput,
) (*apiclient.DurableExecution, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	execution, err := authenticated.API.StartDurableExecution(ctx, authenticated.ProjectID, identifier, input)
	if err != nil {
		return nil, durableFunctionError("start an execution of", identifier, err)
	}
	return execution, nil
}

// ListExecutions returns one execution page for a durable function. An empty
// status lists every execution.
func (s Service) ListExecutions(
	ctx context.Context,
	identifier, status string,
	page, limit int,
) (*apiclient.PaginatedDurableExecutions, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	executions, err := authenticated.API.ListDurableExecutions(
		ctx, authenticated.ProjectID, identifier, status, page, limit)
	if err != nil {
		return nil, durableFunctionError("list executions of", identifier, err)
	}
	return executions, nil
}

// GetExecution returns one execution, including its result once it has
// finished.
func (s Service) GetExecution(
	ctx context.Context,
	identifier string,
	executionID uuid.UUID,
) (*apiclient.DurableExecution, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	execution, err := authenticated.API.GetDurableExecution(ctx, authenticated.ProjectID, identifier, executionID)
	if err != nil {
		return nil, durableExecutionError("get", identifier, executionID, err)
	}
	return execution, nil
}

// StopExecution stops one execution. Steps already completed are not undone.
func (s Service) StopExecution(
	ctx context.Context,
	identifier string,
	executionID uuid.UUID,
) (*apiclient.DurableExecution, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	execution, err := authenticated.API.StopDurableExecution(ctx, authenticated.ProjectID, identifier, executionID)
	if err != nil {
		return nil, durableExecutionError("stop", identifier, executionID, err)
	}
	return execution, nil
}

// ListSchedulers returns the schedulers of one durable function.
func (s Service) ListSchedulers(
	ctx context.Context,
	identifier string,
) (*apiclient.FunctionSchedulerListResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	schedulers, err := authenticated.API.ListDurableFunctionSchedulers(ctx, authenticated.ProjectID, identifier)
	if err != nil {
		return nil, durableFunctionError("list schedulers of", identifier, err)
	}
	return schedulers, nil
}

// CreateScheduler adds a scheduler to one durable function. Each tick starts an
// execution rather than invoking the function.
func (s Service) CreateScheduler(
	ctx context.Context,
	identifier string,
	input api.FunctionSchedulerInput,
) (*apiclient.FunctionScheduler, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	scheduler, err := authenticated.API.CreateDurableFunctionScheduler(
		ctx, authenticated.ProjectID, identifier, input)
	if err != nil {
		return nil, durableFunctionError("create a scheduler for", identifier, err)
	}
	return scheduler, nil
}

// EnableScheduler resumes a scheduler's ticks.
func (s Service) EnableScheduler(
	ctx context.Context, identifier string, schedulerID uuid.UUID,
) (*apiclient.FunctionScheduler, error) {
	return s.setSchedulerEnabled(ctx, identifier, schedulerID, true)
}

// DisableScheduler stops a scheduler's ticks without deleting it. Executions
// already started keep running.
func (s Service) DisableScheduler(
	ctx context.Context, identifier string, schedulerID uuid.UUID,
) (*apiclient.FunctionScheduler, error) {
	return s.setSchedulerEnabled(ctx, identifier, schedulerID, false)
}

func (s Service) setSchedulerEnabled(
	ctx context.Context, identifier string, schedulerID uuid.UUID, enabled bool,
) (*apiclient.FunctionScheduler, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	flag := enabled
	scheduler, err := authenticated.API.UpdateDurableFunctionScheduler(
		ctx, authenticated.ProjectID, identifier, schedulerID, api.FunctionSchedulerInput{Enabled: &flag})
	if err != nil {
		action := "disable"
		if enabled {
			action = "enable"
		}
		return nil, durableSchedulerError(action, identifier, schedulerID, err)
	}
	return scheduler, nil
}

// DeleteScheduler removes a scheduler and its run history. Executions it
// already started keep running.
func (s Service) DeleteScheduler(ctx context.Context, identifier string, schedulerID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteDurableFunctionScheduler(
		ctx, authenticated.ProjectID, identifier, schedulerID); err != nil {
		return durableSchedulerError("delete", identifier, schedulerID, err)
	}
	return nil
}

// ListApprovals returns one page of the project's approvals, newest first.
func (s Service) ListApprovals(
	ctx context.Context, input api.DurableApprovalListInput,
) (*apiclient.PaginatedDurableApprovals, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	approvals, err := authenticated.API.ListDurableApprovals(ctx, authenticated.ProjectID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list approvals: %w", err)
	}
	return approvals, nil
}

// GetApproval returns one approval of the current project.
func (s Service) GetApproval(ctx context.Context, approvalID uuid.UUID) (*apiclient.DurableApproval, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	approval, err := authenticated.API.GetDurableApproval(ctx, authenticated.ProjectID, approvalID)
	if err != nil {
		return nil, approvalError("get", approvalID, err)
	}
	return approval, nil
}

// ApprovalStats counts the project's approvals by outcome between from and
// to. A nil to is the API's now, and a nil from 30 days before to.
func (s Service) ApprovalStats(
	ctx context.Context, function string, from, to *time.Time,
) (*apiclient.DurableApprovalStats, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stats, err := authenticated.API.GetDurableApprovalStats(ctx, authenticated.ProjectID, function, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get approval stats: %w", err)
	}
	return stats, nil
}

// DecideApproval approves or denies one approval, and returns it as decided.
// decision is apiclient.DurableApprovalStatusApproved or
// apiclient.DurableApprovalStatusDenied.
//
// The API answers a conflicting decision with 409 and no detail of what got
// there first, so the approval is read again to say who decided it, or when it
// expired.
func (s Service) DecideApproval(
	ctx context.Context, approvalID uuid.UUID, decision apiclient.DurableApprovalStatus, comment string,
) (*apiclient.DurableApproval, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	decide := authenticated.API.ApproveDurableApproval
	action := "approve"
	if decision == apiclient.DurableApprovalStatusDenied {
		decide = authenticated.API.DenyDurableApproval
		action = "deny"
	}

	approval, err := decide(ctx, authenticated.ProjectID, approvalID, comment)
	if err == nil {
		return approval, nil
	}
	switch api.Status(err) {
	case http.StatusForbidden:
		if api.LastRefusal().ProjectAccessToken || refusesProjectTokenDecision(api.Message(err)) {
			return nil, errApprovalNeedsAPerson
		}
	case http.StatusConflict:
		current, getErr := authenticated.API.GetDurableApproval(ctx, authenticated.ProjectID, approvalID)
		if getErr == nil {
			if conflict := ApprovalConflict(current, decision); conflict != nil {
				return nil, conflict
			}
		}
	}
	return nil, approvalError(action, approvalID, err)
}

// errApprovalNeedsAPerson is the answer to a project access token deciding.
// Approvals exist so that a person signs off, which is why the API refuses
// any credential that is not a person's.
var errApprovalNeedsAPerson = errors.New(
	"approvals are decided by a person. Run `volcano login`, or decide in the dashboard")

// refusesProjectTokenDecision reports whether a 403 message is the API
// refusing a project access token's decision. A read-only token is refused
// before the decision check, but a full one would be refused next, so both
// get the same answer.
func refusesProjectTokenDecision(message string) bool {
	return strings.Contains(message, "cannot decide durable approvals") ||
		strings.Contains(message, "project access token is read-only")
}

// ApprovalConflict says why decision cannot be applied to approval, or returns
// nil when it can: the approval is pending with its deadline ahead, or already
// carries that same decision, which the API accepts again unchanged.
func ApprovalConflict(approval *apiclient.DurableApproval, decision apiclient.DurableApprovalStatus) error {
	if approval == nil || approval.Status == decision {
		return nil
	}

	switch approval.Status {
	case apiclient.DurableApprovalStatusPending:
		// The API refuses a decision once the deadline passes, even before the
		// background pass marks the approval expired.
		if approval.ExpiresAt != nil && !time.Now().Before(*approval.ExpiresAt) {
			return approvalExpired(approval)
		}
		return nil
	case apiclient.DurableApprovalStatusApproved, apiclient.DurableApprovalStatusDenied:
		return fmt.Errorf("approval %s was already %s%s", approval.Id, approval.Status, DecisionSummary(approval.Decision))
	case apiclient.DurableApprovalStatusExpired:
		return approvalExpired(approval)
	case apiclient.DurableApprovalStatusCancelled:
		return fmt.Errorf("approval %s was cancelled: its execution ended before anyone decided", approval.Id)
	default:
		return fmt.Errorf("approval %s is %s and can no longer be decided", approval.Id, approval.Status)
	}
}

func approvalExpired(approval *apiclient.DurableApproval) error {
	if approval.ExpiresAt != nil {
		return fmt.Errorf("approval %s expired at %s before anyone decided",
			approval.Id, approval.ExpiresAt.Local().Format(time.RFC3339))
	}
	return fmt.Errorf("approval %s expired before anyone decided", approval.Id)
}

// DecisionSummary renders who decided and when as " by <email> at <time>", or
// "" when there is no decision.
func DecisionSummary(decision *apiclient.DurableApprovalDecision) string {
	if decision == nil {
		return ""
	}
	decider := "a deleted user"
	if decision.DecidedBy != nil && decision.DecidedBy.Email != "" {
		decider = theme.StripControl(decision.DecidedBy.Email)
	}
	return fmt.Sprintf(" by %s at %s", decider, decision.DecidedAt.Local().Format(time.RFC3339))
}

// approvalError answers 404 in the project's terms: the API gives the same
// answer for an id that never existed and one that belongs to another project.
func approvalError(action string, approvalID uuid.UUID, err error) error {
	if api.Status(err) == http.StatusNotFound {
		return fmt.Errorf("no approval %s in this project", approvalID)
	}
	return fmt.Errorf("failed to %s approval %s: %w", action, approvalID, err)
}

// durableFunctionError names the function the user asked for. The API answers
// 404 for a standard function's name as well as for an unknown one, since the
// two collections are separate, so the message says which one was searched.
func durableFunctionError(action, identifier string, err error) error {
	if api.Status(err) == http.StatusNotFound {
		return fmt.Errorf("durable function %q not found", identifier)
	}
	return fmt.Errorf("failed to %s durable function %q: %w", action, identifier, err)
}

// durableExecutionError names both the execution and the function it was asked
// for. An execution route answers 404 for an unknown function as readily as for
// an unknown execution, and those are different mistakes; the API says which,
// so naming both leaves its answer meaning something.
func durableExecutionError(action, identifier string, executionID uuid.UUID, err error) error {
	return fmt.Errorf("failed to %s durable execution %q of durable function %q: %w",
		action, executionID.String(), identifier, err)
}

// durableSchedulerError names both subjects for the same reason
// durableExecutionError does: a scheduler route answers 404 for an unknown
// durable function as readily as for an unknown scheduler.
func durableSchedulerError(action, identifier string, schedulerID uuid.UUID, err error) error {
	return fmt.Errorf("failed to %s scheduler %q of durable function %q: %w",
		action, schedulerID.String(), identifier, err)
}
