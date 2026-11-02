package output

import (
	"fmt"
	"io"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Deployments renders the project's deployment feed, newest first.
func Deployments(w io.Writer, deployments []apiclient.ProjectDeployment, more bool) {
	if len(deployments) == 0 {
		fmt.Fprintln(w, "No deployments")
		return
	}
	on := theme.On(w)
	tableHead(w, on, false, 120, "%-20s  %-28s  %-8s  %-12s  %-6s  %-8s  %s",
		"Created", "Resource", "Op", "Status", "Source", "Duration", "ID")
	for _, deployment := range deployments {
		fmt.Fprintf(w, "%-20s  %-28s  %-8s  %s  %-6s  %-8s  %s\n",
			FormatTimeAgo(deployment.CreatedAt),
			Truncate(DeploymentResource(deployment.Resource), 28),
			deployment.Operation,
			statusCell(string(deployment.Status), 12, on),
			deployment.DeploySource,
			deploymentDuration(deployment),
			deployment.Id)
	}
	if more {
		summary(w, on, "Showing the newest %d. Raise --limit or narrow with --type, --name or --since.", len(deployments))
		return
	}
	summary(w, on, "Total: %d deployment(s)", len(deployments))
}

// DeploymentResource names the resource a deployment belongs to, with its kind:
// "function checkout", "durable function pipeline" or "frontend web".
func DeploymentResource(resource apiclient.ProjectDeploymentResource) string {
	if resource.Kind != nil && *resource.Kind == apiclient.Durable {
		return "durable function " + resource.Name
	}
	return string(resource.Type) + " " + resource.Name
}

// Deployment renders one deployment: what it deployed, how it went, each
// phase, and where its build logs are.
func Deployment(w io.Writer, deployment *apiclient.ProjectDeployment, logsCommand string) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", deployment.Id)
	kv(w, on, "Resource", "%s (%s)", DeploymentResource(deployment.Resource), deployment.Resource.Id)
	kv(w, on, "Operation", "%s", deployment.Operation)
	kv(w, on, "Status", "%s", theme.Status(string(deployment.Status), on))
	kv(w, on, "Trigger", "%s", deployment.DeploySource)
	kv(w, on, "Created", "%s", FormatTimestamp(deployment.CreatedAt))
	if deployment.CompletedAt != nil {
		kv(w, on, "Completed", "%s", FormatTimestamp(*deployment.CompletedAt))
	}
	kv(w, on, "Duration", "%s", deploymentDuration(*deployment))
	if message := stringPtrValue(deployment.ErrorMessage); message != "" {
		kv(w, on, "Error", "%s", message)
	}
	if progress := deployment.Progress; progress != nil && len(progress.Phases) > 0 {
		fmt.Fprintln(w)
		tableHead(w, on, false, 36, "%-14s  %-12s  %s", "Phase", "Status", "Duration")
		for _, phase := range progress.Phases {
			duration := "-"
			if phase.DurationSeconds != nil {
				duration = (time.Duration(*phase.DurationSeconds) * time.Second).String()
			}
			fmt.Fprintf(w, "%-14s  %s  %s\n", phase.Name, statusCell(string(phase.Status), 12, on), duration)
		}
	}
	if logsCommand != "" {
		fmt.Fprintf(w, "\n%s%s\n", theme.Dim("Build logs: ", on), theme.Command(logsCommand, on))
	}
}

// DeploymentSummary renders the outcome counts of a resource type's
// deployments.
func DeploymentSummary(w io.Writer, summary *apiclient.ProjectDeploymentSummary) {
	on := theme.On(w)
	kv(w, on, "Deployments", "%d", summary.DeploymentCount)
	kv(w, on, "Succeeded", "%d", summary.SuccessfulCount)
	kv(w, on, "Failed", "%d", summary.FailedCount)
	kv(w, on, "Superseded", "%d", summary.CanceledCount)
	successRate := "-"
	if summary.SuccessRate != nil {
		successRate = fmt.Sprintf("%.1f%%", *summary.SuccessRate*100)
	}
	kv(w, on, "Success rate", "%s", successRate)
	median := "-"
	if summary.MedianBuildDurationSeconds != nil {
		median = (time.Duration(*summary.MedianBuildDurationSeconds * float64(time.Second))).Round(time.Second).String()
	}
	kv(w, on, "Median build", "%s", median)
}

// deploymentDuration is how long a deployment took, or has taken so far.
func deploymentDuration(deployment apiclient.ProjectDeployment) string {
	switch {
	case deployment.CompletedAt != nil:
		return deployment.CompletedAt.Sub(deployment.CreatedAt).Round(time.Second).String()
	case deployment.Progress != nil:
		return (time.Duration(deployment.Progress.ElapsedSeconds) * time.Second).String()
	default:
		return "-"
	}
}
