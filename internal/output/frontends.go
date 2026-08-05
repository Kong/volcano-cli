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

// Frontend renders one frontend detail view.
func Frontend(w io.Writer, fe *apiclient.Frontend) {
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

	if domain.RequiredRoutingRecord != nil {
		fmt.Fprintln(w, theme.Dim("Required routing record:", on))
		fmt.Fprintf(w, "  %s %s -> %s\n",
			domain.RequiredRoutingRecord.RecordType,
			domain.RequiredRoutingRecord.Name,
			domain.RequiredRoutingRecord.Value,
		)
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
