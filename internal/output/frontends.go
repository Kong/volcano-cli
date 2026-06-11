package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Frontends renders one frontend list page.
func Frontends(w io.Writer, page *apiclient.PaginatedFrontends) {
	if page == nil {
		page = &apiclient.PaginatedFrontends{}
	}

	frontends := page.Data
	if len(frontends) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No frontends deployed")
			return
		}
		fmt.Fprintf(w, "No frontends found on page %d\n", page.Page)
		printFrontendPageSummary(w, page)
		return
	}

	fmt.Fprintf(w, "\n%-20s  %-12s  %-15s  %-15s\n", "Name", "Status", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 70))
	for _, fe := range frontends {
		fmt.Fprintf(w, "%-20s  %-12s  %-15s  %-15s\n",
			Truncate(fe.Name, 20),
			frontendStatus(fe),
			FormatTimeAgo(fe.CreatedAt),
			FormatTimeAgo(fe.UpdatedAt),
		)
		if siteURL := stringPtrValue(fe.SiteUrl); siteURL != "" {
			fmt.Fprintf(w, "  site: %s\n", siteURL)
		}
	}
	printFrontendPageSummary(w, page)
	if page.HasMore {
		fmt.Fprintf(w, "\nNext page: volcano cloud frontends list --page %d --limit %d\n", page.Page+1, page.Limit)
	}
}

func printFrontendPageSummary(w io.Writer, page *apiclient.PaginatedFrontends) {
	fmt.Fprintf(w, "\nShowing %d of %d frontend(s) (page %d, limit %d)\n", len(page.Data), page.Total, page.Page, page.Limit)
}

// Frontend renders one frontend detail view.
func Frontend(w io.Writer, fe *apiclient.Frontend) {
	if fe == nil {
		return
	}
	fmt.Fprintf(w, "ID: %s\n", fe.Id.String())
	fmt.Fprintf(w, "Name: %s\n", fe.Name)
	fmt.Fprintf(w, "Framework: %s\n", strings.TrimSpace(string(fe.Framework)))
	fmt.Fprintf(w, "Status: %s\n", frontendStatus(*fe))
	if appRoot := stringPtrValue(fe.AppRoot); appRoot != "" {
		fmt.Fprintf(w, "App Root: %s\n", appRoot)
	}
	if len(fe.DeployedRegions) > 0 {
		fmt.Fprintf(w, "Regions: %s\n", strings.Join(fe.DeployedRegions, ", "))
	}
	if siteURL := stringPtrValue(fe.SiteUrl); siteURL != "" {
		fmt.Fprintf(w, "Site URL: %s\n", siteURL)
	}
	if customDomain := stringPtrValue(fe.CustomDomain); customDomain != "" {
		fmt.Fprintf(w, "Custom Domain: %s\n", customDomain)
	}
	if fe.CurrentDeploymentId != nil {
		fmt.Fprintf(w, "Current Deployment: %s\n", fe.CurrentDeploymentId.String())
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(fe.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(fe.UpdatedAt))
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
	fmt.Fprintf(w, "Domain: %s\n", domain.Domain)
	fmt.Fprintf(w, "TLS mode: %s\n", strings.TrimSpace(string(domain.TlsMode)))
	fmt.Fprintf(w, "Domain status: %s\n", strings.TrimSpace(string(domain.DomainStatus)))
	fmt.Fprintf(w, "Verification status: %s\n", strings.TrimSpace(string(domain.VerificationStatus)))

	if domain.RequiredRoutingRecord != nil {
		fmt.Fprintln(w, "Required routing record:")
		fmt.Fprintf(w, "  %s %s -> %s\n",
			domain.RequiredRoutingRecord.RecordType,
			domain.RequiredRoutingRecord.Name,
			domain.RequiredRoutingRecord.Value,
		)
	}
	if domain.VerificationRecords != nil && len(*domain.VerificationRecords) > 0 {
		fmt.Fprintln(w, "Verification records:")
		for _, record := range *domain.VerificationRecords {
			fmt.Fprintf(w, "  %s %s -> %s\n", record.Type, record.Name, record.Value)
		}
	}
	if len(domain.EffectiveUrls) > 0 {
		fmt.Fprintln(w, "Effective URLs:")
		for _, siteURL := range domain.EffectiveUrls {
			fmt.Fprintf(w, "  - %s\n", siteURL)
		}
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(domain.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(domain.UpdatedAt))
}

// FrontendCustomDomains renders custom domains configured for frontends.
func FrontendCustomDomains(w io.Writer, entries []FrontendCustomDomainEntry) {
	if len(entries) == 0 {
		fmt.Fprintln(w, "No custom domains configured")
		return
	}

	fmt.Fprintf(w, "\n%-32s  %-38s  %-32s  %-22s  %-15s  %-15s\n", "Frontend", "Frontend ID", "Domain", "Status", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 164))
	for _, entry := range entries {
		fmt.Fprintf(w, "%-32s  %-38s  %-32s  %-22s  %-15s  %-15s\n",
			Truncate(entry.FrontendName, 32),
			entry.FrontendID,
			Truncate(entry.Domain.Domain, 32),
			strings.TrimSpace(string(entry.Domain.DomainStatus)),
			FormatTimeAgo(entry.Domain.CreatedAt),
			FormatTimeAgo(entry.Domain.UpdatedAt),
		)
	}
	fmt.Fprintf(w, "\nTotal: %d custom domain(s)\n", len(entries))
}

func frontendStatus(fe apiclient.Frontend) string {
	status := strings.TrimSpace(string(fe.Status))
	if status == "" {
		return "-"
	}
	return status
}
