package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Frontends renders one frontend list page.
func Frontends(w io.Writer, page *apiclient.PaginatedFrontends, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedFrontends{}
	}

	on := theme.On(w)
	frontends := page.Data
	if len(frontends) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No frontends deployed")
			return
		}
		fmt.Fprintf(w, "No frontends found on page %d\n", page.Page)
		printFrontendPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 70, "%-20s  %-12s  %-15s  %-15s", "Name", "Status", "Created", "Updated")
	for _, fe := range frontends {
		fmt.Fprintf(w, "%-20s  %s  %-15s  %-15s\n",
			Truncate(fe.Name, 20),
			statusCell(frontendStatus(fe), 12, on),
			FormatTimeAgo(fe.CreatedAt),
			FormatTimeAgo(fe.UpdatedAt),
		)
		if siteURL := stringPtrValue(fe.SiteUrl); siteURL != "" {
			fmt.Fprintf(w, "  %s %s\n", theme.Dim("site:", on), siteURL)
		}
	}
	printFrontendPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s frontends list --page %d --limit %d", commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printFrontendPageSummary(w io.Writer, on bool, page *apiclient.PaginatedFrontends) {
	summary(w, on, "Showing %d of %d frontend(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

// FrontendRouteEntry is one frontend function route and the function it forwards to.
type FrontendRouteEntry struct {
	ID          string
	PathPrefix  string
	StripPrefix bool
	// Function is the target's name, or its ID when the name is unknown.
	Function string
	// Visibility is empty when the target is unknown.
	Visibility string
}

// Frontend renders one frontend detail view and its function routes.
func Frontend(w io.Writer, fe *apiclient.Frontend, routes []FrontendRouteEntry) {
	if fe == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", fe.Id.String())
	kv(w, on, "Name", "%s", fe.Name)
	kv(w, on, "Framework", "%s", strings.TrimSpace(string(fe.Framework)))
	kv(w, on, "Status", "%s", theme.Status(frontendStatus(*fe), on))
	if appRoot := stringPtrValue(fe.AppRoot); appRoot != "" {
		kv(w, on, "App Root", "%s", appRoot)
	}
	if len(fe.DeployedRegions) > 0 {
		kv(w, on, "Regions", "%s", strings.Join(fe.DeployedRegions, ", "))
	}
	if siteURL := stringPtrValue(fe.SiteUrl); siteURL != "" {
		kv(w, on, "Site URL", "%s", siteURL)
	}
	if customDomain := stringPtrValue(fe.CustomDomain); customDomain != "" {
		kv(w, on, "Custom Domain", "%s", customDomain)
	}
	if fe.CurrentDeploymentId != nil {
		kv(w, on, "Current Deployment", "%s", fe.CurrentDeploymentId.String())
	}
	if fe.PendingDeploymentId != nil {
		kv(w, on, "Pending Deployment", "%s", fe.PendingDeploymentId.String())
	}
	kv(w, on, "Created", "%s", FormatTimestamp(fe.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(fe.UpdatedAt))
	if len(routes) > 0 {
		fmt.Fprintln(w, theme.Dim("Function routes:", on))
		for _, route := range routes {
			fmt.Fprintf(w, "  %s -> %s (%s)\n", route.PathPrefix, route.Function, routeDetails(route, on))
		}
	}
}

// FrontendRoutes renders a frontend's function routes.
func FrontendRoutes(w io.Writer, frontend string, routes []FrontendRouteEntry) {
	if len(routes) == 0 {
		fmt.Fprintf(w, "No function routes on frontend %q\n", frontend)
		return
	}

	on := theme.On(w)
	tableHead(w, on, false, 120, "%-32s  %-24s  %-13s  %-5s  %-36s", "Path", "Function", "Visibility", "Strip", "ID")
	for _, route := range routes {
		fmt.Fprintf(w, "%-32s  %-24s  %s  %-5s  %-36s\n",
			Truncate(route.PathPrefix, 32),
			Truncate(route.Function, 24),
			statusCell(blankString(route.Visibility), 13, on),
			formatBool(route.StripPrefix),
			route.ID,
		)
	}
	summary(w, on, "Total: %d route(s)", len(routes))
}

// FrontendRoute renders one frontend function route.
func FrontendRoute(w io.Writer, route FrontendRouteEntry) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", route.ID)
	kv(w, on, "Path", "%s", route.PathPrefix)
	kv(w, on, "Function", "%s", route.Function)
	kv(w, on, "Visibility", "%s", theme.Status(blankString(route.Visibility), on))
	kv(w, on, "Strip prefix", "%s", formatBool(route.StripPrefix))
}

func routeDetails(route FrontendRouteEntry, on bool) string {
	details := theme.Status(blankString(route.Visibility), on)
	if route.StripPrefix {
		details += ", strip prefix"
	}
	return details
}

// FrontendCustomDomainEntry contains one frontend custom domain row.
type FrontendCustomDomainEntry struct {
	FrontendName string
	FrontendID   string
	Domain       apiclient.FrontendCustomDomainResponse
}

// FrontendCustomDomain renders one custom domain detail view.
func FrontendCustomDomain(w io.Writer, domain *apiclient.FrontendCustomDomainResponse) {
	if domain == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Domain", "%s", domain.Domain)
	kv(w, on, "TLS mode", "%s", strings.TrimSpace(string(domain.TlsMode)))
	kv(w, on, "Domain status", "%s", theme.Status(strings.TrimSpace(string(domain.DomainStatus)), on))
	kv(w, on, "Verification status", "%s", theme.Status(strings.TrimSpace(string(domain.VerificationStatus)), on))

	if domain.RoutingTargetHostname != nil && *domain.RoutingTargetHostname != "" {
		kv(w, on, "DNS routing target", "%s", *domain.RoutingTargetHostname)
		fmt.Fprintln(w, "  Use CNAME only if your DNS provider confirms this is not a zone apex. At an apex, use provider-supported ALIAS, ANAME, or CNAME flattening.")
	}
	if domain.VerificationRecords != nil && len(*domain.VerificationRecords) > 0 {
		fmt.Fprintln(w, theme.Dim("Verification records:", on))
		for _, record := range *domain.VerificationRecords {
			fmt.Fprintf(w, "  %s %s -> %s\n", record.Type, record.Name, record.Value)
		}
	}
	if len(domain.EffectiveUrls) > 0 {
		fmt.Fprintln(w, theme.Dim("Effective URLs:", on))
		for _, siteURL := range domain.EffectiveUrls {
			fmt.Fprintf(w, "  - %s\n", siteURL)
		}
	}
	kv(w, on, "Created", "%s", FormatTimestamp(domain.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(domain.UpdatedAt))
}

// FrontendCustomDomains renders custom domains configured for frontends.
func FrontendCustomDomains(w io.Writer, entries []FrontendCustomDomainEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No custom domains configured")
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 164, "%-32s  %-38s  %-32s  %-22s  %-15s  %-15s", "Frontend", "Frontend ID", "Domain", "Status", "Created", "Updated")
	for _, entry := range entries {
		fmt.Fprintf(w, "%-32s  %-38s  %-32s  %s  %-15s  %-15s\n",
			Truncate(entry.FrontendName, 32),
			entry.FrontendID,
			Truncate(entry.Domain.Domain, 32),
			statusCell(strings.TrimSpace(string(entry.Domain.DomainStatus)), 22, on),
			FormatTimeAgo(entry.Domain.CreatedAt),
			FormatTimeAgo(entry.Domain.UpdatedAt),
		)
	}
	summary(w, on, "Total: %d custom domain(s)", len(entries))
}

func frontendStatus(fe apiclient.Frontend) string {
	status := strings.TrimSpace(string(fe.Status))
	if status == "" {
		return "-"
	}
	return status
}
