package output

import (
	"fmt"
	"io"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// AccessTokenCreated renders a newly minted project access token, including
// the plaintext secret the API returns only once.
func AccessTokenCreated(w io.Writer, token *apiclient.CreatedProjectAccessToken) {
	on := theme.On(w)
	Success(w, "Access token '%s' created", token.Name)
	kv(w, on, "ID", "%s", token.Id.String())
	kv(w, on, "Scope", "%s", string(token.Scope))
	kv(w, on, "Expires", "%s", accessTokenExpiry(token.ExpiresAt))
	fmt.Fprintf(w, "\n%s %s\n", theme.Dim("Token:", on), token.Token)
	Warning(w, "Copy this token now. It is shown once and cannot be retrieved again.")
}

// AccessTokens renders one project access token list page.
func AccessTokens(w io.Writer, page *apiclient.PaginatedProjectAccessTokens, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedProjectAccessTokens{}
	}

	on := theme.On(w)
	tokens := page.Data
	if len(tokens) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No access tokens created")
		} else {
			fmt.Fprintf(w, "No access tokens found on page %d\n", page.Page)
		}
		printAccessTokenPageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 96, "%-24s  %-16s  %-10s  %-9s  %-15s  %-12s",
		"Name", "Prefix", "Scope", "Status", "Last used", "Requests")
	for _, token := range tokens {
		fmt.Fprintf(w, "%-24s  %-16s  %-10s  %s  %-15s  %-12d\n",
			Truncate(token.Name, 24),
			Truncate(token.TokenPrefix, 16),
			string(token.Scope),
			statusCell(string(token.Status), 9, on),
			accessTokenLastUsed(token.LastUsedAt),
			token.AllTimeRequests,
		)
	}
	printAccessTokenPageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s access-tokens list --page %d --limit %d", commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printAccessTokenPageSummary(w io.Writer, on bool, page *apiclient.PaginatedProjectAccessTokens) {
	summary(w, on, "Showing %d of %d access token(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

// AccessToken renders one project access token.
func AccessToken(w io.Writer, token *apiclient.ProjectAccessToken) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", token.Id.String())
	kv(w, on, "Name", "%s", token.Name)
	kv(w, on, "Prefix", "%s", token.TokenPrefix)
	kv(w, on, "Scope", "%s", string(token.Scope))
	kv(w, on, "Status", "%s", theme.Status(string(token.Status), on))
	if source := stringPtrValue(token.TokenSource); source != "" {
		kv(w, on, "Created by", "%s", source)
	}
	kv(w, on, "Expires", "%s", accessTokenExpiry(token.ExpiresAt))
	kv(w, on, "Last used", "%s", accessTokenLastUsed(token.LastUsedAt))
	kv(w, on, "Requests", "%d", token.AllTimeRequests)
	kv(w, on, "Created", "%s", FormatTimestamp(token.CreatedAt))
}

// AccessTokenUsage renders the daily request counts for one access token. The
// series is zero-filled by the API, so every day in the window prints.
func AccessTokenUsage(w io.Writer, usage *apiclient.ProjectAccessTokenUsage) {
	if usage == nil {
		return
	}

	on := theme.On(w)
	if len(usage.Daily) == 0 {
		summary(w, on, "No usage recorded for '%s'", usage.Name)
		return
	}

	tableHead(w, on, true, 24, "%-12s  %-10s", "Day", "Requests")
	for _, entry := range usage.Daily {
		fmt.Fprintf(w, "%-12s  %-10d\n", entry.Day.Format(time.DateOnly), entry.Requests)
	}
	summary(w, on, "%d request(s) over %d day(s)", usage.TotalRequests, usage.Days)
}

// AccessTokensUsage renders the windowed request count of every access token in
// a project.
func AccessTokensUsage(w io.Writer, usage []apiclient.ProjectAccessTokenUsage) {
	on := theme.On(w)
	if len(usage) == 0 {
		fmt.Fprintln(w, "No access tokens created")
		return
	}

	tableHead(w, on, true, 38, "%-24s  %-12s", "Name", "Requests")
	var total int64
	for _, entry := range usage {
		total += entry.TotalRequests
		fmt.Fprintf(w, "%-24s  %-12d\n", Truncate(entry.Name, 24), entry.TotalRequests)
	}
	summary(w, on, "%d request(s) across %d token(s) over %d day(s)", total, len(usage), usage[0].Days)
}

func accessTokenExpiry(expiresAt *time.Time) string {
	if expiresAt == nil {
		return "never"
	}
	return FormatTimestamp(*expiresAt)
}

func accessTokenLastUsed(lastUsedAt *time.Time) string {
	if lastUsedAt == nil {
		return "never"
	}
	return FormatTimeAgo(*lastUsedAt)
}
