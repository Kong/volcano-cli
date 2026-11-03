package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/project"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	pollInterval = 5 * time.Second
	waitTimeout  = 30 * time.Minute
	// clockSkew widens the deployment listing so a client clock ahead of the
	// server's does not hide the deployments this run started.
	clockSkew = time.Hour
	// schedulersSection is the manifest section the server files scheduler
	// errors under, each named "<function>/<scheduler>".
	schedulersSection = "functions.schedulers"
)

type resourceKind string

const (
	kindFunction resourceKind = "function"
	kindDurable  resourceKind = "durable function"
	kindFrontend resourceKind = "frontend"
)

type startedDeployment struct {
	kind     resourceKind
	name     string
	resource uuid.UUID
	// id is uuid.Nil when the deploy response named no deployment.
	id uuid.UUID
}

type runner struct {
	opts    options
	started []startedDeployment
	// deferred are the functions whose schedulers the config step left out
	// because the functions were not active yet.
	deferred []startedDeployment
	// schedulers holds just those functions' schedulers, for the scheduler pass
	// after the wait. configCommands apply every manifest again, for the user to
	// run when that pass does not.
	schedulers     *projectconfig.Manifest
	configCommands []string
}

// track records a deployment to wait for. A deploy queued behind another one
// reports it as pending; otherwise the current deployment is the new one.
func (r *runner) track(kind resourceKind, name string, resource uuid.UUID, current, pending *uuid.UUID) {
	d := startedDeployment{kind: kind, name: name, resource: resource}
	switch {
	case pending != nil:
		d.id = *pending
	case current != nil:
		d.id = *current
	}
	r.started = append(r.started, d)
}

// wait polls the project's deployments until every one this run started has
// finished, then prints the frontend URLs. Resource status alone can't tell:
// a frontend whose redeploy fails keeps serving, and stays active.
func (r *runner) wait(ctx context.Context, start time.Time) error {
	out := r.opts.out
	on := theme.On(out)
	if len(r.started) == 0 {
		return nil
	}
	fmt.Fprintf(out, "\nWaiting for %d deployment(s)...\n", len(r.started))

	width := 0
	for _, d := range r.started {
		width = max(width, len(d.name))
	}
	pending := map[uuid.UUID]startedDeployment{}
	for _, d := range r.started {
		if d.id == uuid.Nil {
			fmt.Fprintf(out, "  - %-*s  %s\n", width, d.name, theme.Dim("no deployment reported", on))
			continue
		}
		pending[d.id] = d
	}

	projects := project.NewService(r.opts.deps)
	ticker := cliruntime.NewTicker(r.opts.deps, pollInterval)
	defer ticker.Stop()
	timeout := cliruntime.NewTimer(r.opts.deps, waitTimeout)
	defer timeout.Stop()

	var failed []startedDeployment
	var succeeded []startedDeployment
	for len(pending) > 0 {
		deployments, err := projects.DeploymentsSince(ctx, start.Add(-clockSkew))
		if err != nil {
			return err
		}
		listed := make(map[uuid.UUID]apiclient.ProjectDeployment, len(deployments))
		for _, d := range deployments {
			listed[d.Id] = d
		}
		for i, d := range r.started {
			if _, waiting := pending[d.id]; !waiting {
				continue
			}
			deployment, ok := listed[d.id]
			if !ok {
				// Not created during this run, so the deploy changed nothing.
				fmt.Fprintf(out, "  - %-*s  %s\n", width, d.name, theme.Dim("unchanged", on))
				delete(pending, d.id)
				continue
			}
			switch deployment.Status {
			case apiclient.ProjectDeploymentStatusActive, apiclient.ProjectDeploymentStatusSuperseded:
				// The config step can redeploy a resource, as on a region change.
				// The server supersedes this run's deployment if it was still
				// queued, or queues the redeploy behind it. Either way the newer
				// one is what serves, so wait for it.
				if next, found := newestDeployment(deployments, deployment); found {
					delete(pending, d.id)
					r.started[i].id = next.Id
					pending[next.Id] = r.started[i]
					continue
				}
				if deployment.Status == apiclient.ProjectDeploymentStatusSuperseded {
					fmt.Fprintf(out, "  - %-*s  %s\n", width, d.name, theme.Dim("superseded by a newer deployment", on))
					break
				}
				provisioning, err := r.durableStillProvisioning(ctx, d)
				if err != nil {
					return err
				}
				if provisioning {
					continue
				}
				fmt.Fprintf(out, "  %s %-*s  %s\n", theme.Success("✓", on), width, d.name, deployment.Status)
				succeeded = append(succeeded, d)
			case apiclient.ProjectDeploymentStatusDeleted:
				fmt.Fprintf(out, "  %s %-*s  %s\n", theme.Success("✓", on), width, d.name, deployment.Status)
				succeeded = append(succeeded, d)
			case apiclient.ProjectDeploymentStatusFailed, apiclient.ProjectDeploymentStatusDegraded:
				fmt.Fprintf(out, "  %s %-*s  %s\n", theme.Fail("✗", on), width, d.name, failureText(deployment))
				fmt.Fprintf(out, "    %s\n", theme.Command(r.logsCommand(d), on))
				failed = append(failed, d)
			default:
				continue
			}
			delete(pending, d.id)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C():
			return fmt.Errorf("stopped waiting after %s; still deploying: %s. The deployments continue on the server",
				waitTimeout, pendingNames(r.started, pending))
		case <-ticker.C():
		}
	}

	r.printFrontendURLs(ctx, succeeded)
	if len(failed) > 0 {
		names := make([]string, 0, len(failed))
		for _, d := range failed {
			names = append(names, fmt.Sprintf("%s %s", d.kind, d.name))
		}
		return fmt.Errorf("%d deployment(s) failed: %s", len(failed), strings.Join(names, ", "))
	}
	return nil
}

// inactiveScheduledFunctions returns the functions this run deployed that the
// manifests give schedulers and that are not active yet. The server refuses to
// create or change a scheduler on such a function, and that rejects the whole
// config.
func (r *runner) inactiveScheduledFunctions(ctx context.Context, scheduled map[string]bool) ([]startedDeployment, error) {
	var inactive []startedDeployment
	for _, d := range r.started {
		if (d.kind != kindFunction && d.kind != kindDurable) || !scheduled[d.name] || d.resource == uuid.Nil {
			continue
		}
		status, err := r.functionStatus(ctx, d)
		if err != nil {
			return nil, err
		}
		if status != string(apiclient.FunctionStatusActive) {
			inactive = append(inactive, d)
		}
	}
	return inactive, nil
}

// refusedSchedulers dry-runs each manifest and returns the inactive functions
// whose schedulers the server refuses only because the function is not
// deployed yet. An unchanged scheduler is accepted while its function builds,
// so a redeploy usually needs no second pass. A function with any other
// scheduler error, such as an invalid cron, is not held back, so the real
// apply reports that error now. Any other failure is left for the real apply
// too.
func (r *runner) refusedSchedulers(ctx context.Context, manifests []manifestFile, inactive []startedDeployment) []startedDeployment {
	names := map[string]bool{}
	for _, d := range inactive {
		names[d.name] = true
	}
	refused := map[string]bool{}
	invalid := map[string]bool{}
	configs := projectconfig.NewService(r.opts.deps)
	for _, m := range manifests {
		_, err := configs.Deploy(ctx, m.manifest, true)
		validation, ok := errors.AsType[*api.ProjectConfigValidationError](err)
		if !ok {
			continue
		}
		for _, e := range validation.Errors {
			if e.Section != schedulersSection || e.Name == nil {
				continue
			}
			function, _, _ := strings.Cut(*e.Name, "/")
			if !names[function] {
				continue
			}
			if refusedUntilDeployed(e.Message) {
				refused[function] = true
			} else {
				invalid[function] = true
			}
		}
	}
	var held []startedDeployment
	for _, d := range inactive {
		if refused[d.name] && !invalid[d.name] {
			held = append(held, d)
		}
	}
	return held
}

// refusedUntilDeployed reports whether a scheduler error is one the server
// stops returning once the function is active with deployed regions.
func refusedUntilDeployed(message string) bool {
	return strings.Contains(message, "active function") || strings.Contains(message, "no deployed regions")
}

// applyDeferredSchedulers applies the schedulers the config step left out, once
// their functions are active. It applies only those schedulers, so values a
// later manifest set stay as the config step left them.
func (r *runner) applyDeferredSchedulers(ctx context.Context) error {
	if len(r.deferred) == 0 {
		return nil
	}
	fmt.Fprintf(r.opts.out, "\n%s\n", theme.Title("Applying schedulers", theme.On(r.opts.out)))
	if err := r.waitForActive(ctx, r.deferred); err != nil {
		return err
	}
	result, err := projectconfig.NewService(r.opts.deps).Deploy(ctx, r.schedulers, false)
	if err != nil {
		if validation, ok := errors.AsType[*api.ProjectConfigValidationError](err); ok {
			output.ProjectConfigValidationErrors(r.opts.out, validation.Errors)
		}
		return err
	}
	output.Success(r.opts.out, "Schedulers applied for %s", strings.Join(deploymentNames(r.deferred), ", "))
	// The manifest names only these functions, so every other one would be
	// reported as not covered by it.
	result.Missing = nil
	output.ProjectConfigApplyReport(r.opts.out, result)
	if result.Summary.Errors > 0 {
		return fmt.Errorf("%d scheduler change(s) failed to apply; see the report above", result.Summary.Errors)
	}
	return nil
}

// schedulerHint says which schedulers were not applied and how to apply them.
func (r *runner) schedulerHint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Schedulers for %s were not applied. Once %s, apply them with:",
		strings.Join(deploymentNames(r.deferred), ", "),
		plural(len(r.deferred), "it is active, if it isn't already", "they are active, if they aren't already"))
	for _, command := range r.configCommands {
		b.WriteString("\n  " + command)
	}
	return b.String()
}

// waitForActive waits until each function is active.
func (r *runner) waitForActive(ctx context.Context, waiting []startedDeployment) error {
	ticker := cliruntime.NewTicker(r.opts.deps, pollInterval)
	defer ticker.Stop()
	timeout := cliruntime.NewTimer(r.opts.deps, waitTimeout)
	defer timeout.Stop()

	announced := false
	for {
		var inactive []startedDeployment
		for _, d := range waiting {
			status, err := r.functionStatus(ctx, d)
			if err != nil {
				return err
			}
			switch status {
			case string(apiclient.FunctionStatusActive):
			case string(apiclient.FunctionStatusFailed):
				return fmt.Errorf("%s %s failed to deploy, so its schedulers can't be applied. See the build log:\n  %s",
					d.kind, d.name, r.logsCommand(d))
			case string(apiclient.FunctionStatusDeleting):
				return fmt.Errorf("%s %s is being deleted, so its schedulers can't be applied", d.kind, d.name)
			default:
				inactive = append(inactive, d)
			}
		}
		if len(inactive) == 0 {
			return nil
		}
		waiting = inactive
		names := deploymentNames(waiting)
		if !announced {
			fmt.Fprintf(r.opts.out, "Waiting for %s to become active...\n", strings.Join(names, ", "))
			announced = true
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C():
			return fmt.Errorf("stopped waiting after %s for %s to become active", waitTimeout, strings.Join(names, ", "))
		case <-ticker.C():
		}
	}
}

func deploymentNames(deployments []startedDeployment) []string {
	names := make([]string, 0, len(deployments))
	for _, d := range deployments {
		names = append(names, d.name)
	}
	return names
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// functionStatus reads the status of a function or durable function this run
// deployed.
func (r *runner) functionStatus(ctx context.Context, d startedDeployment) (string, error) {
	if d.kind == kindDurable {
		fn, err := clidurable.NewService(r.opts.deps).Get(ctx, d.resource.String())
		if err != nil {
			return "", fmt.Errorf("%s: %w", d.name, err)
		}
		return string(fn.Status), nil
	}
	fn, err := clifunction.NewService(r.opts.deps).GetByID(ctx, d.resource)
	if err != nil {
		return "", fmt.Errorf("%s: %w", d.name, err)
	}
	return string(fn.Status), nil
}

// durableStillProvisioning reports whether a durable function whose deployment
// is active still refuses executions. A deployment completes before the server
// re-syncs variables changed while it built, and that sync holds the function
// in provisioning, where a start is refused.
func (r *runner) durableStillProvisioning(ctx context.Context, d startedDeployment) (bool, error) {
	if d.kind != kindDurable {
		return false, nil
	}
	identifier := d.name
	if d.resource != uuid.Nil {
		identifier = d.resource.String()
	}
	fn, err := clidurable.NewService(r.opts.deps).Get(ctx, identifier)
	if err != nil {
		return false, err
	}
	return fn.Status == apiclient.DurableFunctionStatusProvisioning, nil
}

// newestDeployment finds the latest deployment of after's resource that was
// created after it and is not superseded itself.
func newestDeployment(deployments []apiclient.ProjectDeployment, after apiclient.ProjectDeployment) (apiclient.ProjectDeployment, bool) {
	var newest apiclient.ProjectDeployment
	found := false
	for _, d := range deployments {
		if d.Resource.Id != after.Resource.Id || d.Id == after.Id ||
			d.Status == apiclient.ProjectDeploymentStatusSuperseded || d.CreatedAt.Before(after.CreatedAt) {
			continue
		}
		if !found || d.CreatedAt.After(newest.CreatedAt) {
			newest, found = d, true
		}
	}
	return newest, found
}

func failureText(d apiclient.ProjectDeployment) string {
	if d.ErrorMessage != nil && strings.TrimSpace(*d.ErrorMessage) != "" {
		return fmt.Sprintf("%s: %s", d.Status, strings.TrimSpace(*d.ErrorMessage))
	}
	return string(d.Status)
}

func (r *runner) logsCommand(d startedDeployment) string {
	switch d.kind {
	case kindFrontend:
		return cliruntime.CommandPath(r.opts.deps, "frontends logs "+d.name+" --type build")
	case kindDurable:
		return cliruntime.CommandPath(r.opts.deps, "durable logs "+d.name+" --type build")
	default:
		return cliruntime.CommandPath(r.opts.deps, "functions logs "+d.name+" --type build")
	}
}

func pendingNames(started []startedDeployment, pending map[uuid.UUID]startedDeployment) string {
	var names []string
	for _, d := range started {
		if _, ok := pending[d.id]; ok {
			names = append(names, d.name)
		}
	}
	return strings.Join(names, ", ")
}

// printFrontendURLs reads each deployed frontend back for its URL. A URL that
// can't be read is left out rather than failing a deploy that succeeded.
func (r *runner) printFrontendURLs(ctx context.Context, deployed []startedDeployment) {
	frontends := clifrontend.NewService(r.opts.deps)
	var lines []string
	for _, d := range deployed {
		if d.kind != kindFrontend {
			continue
		}
		identifier := d.name
		if d.resource != uuid.Nil {
			identifier = d.resource.String()
		}
		fe, err := frontends.Get(ctx, identifier)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			output.Warning(r.opts.errOut, "could not read the URL of frontend %s: %v", d.name, err)
			continue
		}
		if fe.SiteUrl != nil && *fe.SiteUrl != "" {
			lines = append(lines, fmt.Sprintf("  %s  %s", d.name, *fe.SiteUrl))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(r.opts.out, "\nFrontends:")
	for _, line := range lines {
		fmt.Fprintln(r.opts.out, line)
	}
}
