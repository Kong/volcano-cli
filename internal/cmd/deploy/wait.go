package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	pollInterval = 5 * time.Second
	waitTimeout  = 30 * time.Minute
	// clockSkew widens the deployment listing so a client clock ahead of the
	// server's does not hide the deployments this run started.
	clockSkew = time.Hour
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
			case apiclient.ProjectDeploymentStatusActive, apiclient.ProjectDeploymentStatusDeleted:
				fmt.Fprintf(out, "  %s %-*s  %s\n", theme.Success("✓", on), width, d.name, deployment.Status)
				succeeded = append(succeeded, d)
			case apiclient.ProjectDeploymentStatusSuperseded:
				// The config step supersedes deployments it redeploys, as on a
				// region change; the newer one is what serves, so wait for it.
				if next, found := newestDeployment(deployments, deployment); found {
					delete(pending, d.id)
					r.started[i].id = next.Id
					pending[next.Id] = r.started[i]
					continue
				}
				fmt.Fprintf(out, "  - %-*s  %s\n", width, d.name, theme.Dim("superseded by a newer deployment", on))
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

// newestDeployment finds the latest deployment of superseded's resource that
// was created after it and is not superseded itself.
func newestDeployment(deployments []apiclient.ProjectDeployment, superseded apiclient.ProjectDeployment) (apiclient.ProjectDeployment, bool) {
	var newest apiclient.ProjectDeployment
	found := false
	for _, d := range deployments {
		if d.Resource.Id != superseded.Resource.Id || d.Id == superseded.Id ||
			d.Status == apiclient.ProjectDeploymentStatusSuperseded || d.CreatedAt.Before(superseded.CreatedAt) {
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
