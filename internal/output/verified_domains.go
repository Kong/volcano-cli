package output

import (
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// VerifiedDomains renders the account's verified domains.
func VerifiedDomains(w io.Writer, domains []apiclient.VerifiedDomain) {
	if len(domains) == 0 {
		fmt.Fprintln(w, "No verified domains")
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 68, "%-48s  %-18s", "Domain", "Verified")
	for _, domain := range domains {
		fmt.Fprintf(w, "%-48s  %-18s\n", Truncate(domain.Domain, 48), FormatTimeAgo(domain.VerifiedAt))
	}
	summary(w, on, "Total: %d verified domain(s)", len(domains))
}
