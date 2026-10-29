package output

import (
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	deployLinkBase   = "https://volcano.dev/deploy/"
	deployButtonURL  = "https://volcano.dev/deploy-button.svg"
	deployButtonAlt  = "Deploy to Volcano"
	statsDailyMaxRow = 14
)

// deployLinkKinds is the order stats show kinds in: the path a visitor takes
// from seeing the link to a finished deploy, then the failure.
var deployLinkKinds = []apiclient.DeployLinkKindCountKind{
	apiclient.DeployLinkKindCountKindPreview,
	apiclient.DeployLinkKindCountKindClick,
	apiclient.DeployLinkKindCountKindConfirm,
	apiclient.DeployLinkKindCountKindAccepted,
	apiclient.DeployLinkKindCountKindReady,
	apiclient.DeployLinkKindCountKindFailed,
}

// DeployLinkURL returns the public address of a deploy link.
func DeployLinkURL(id string) string {
	return deployLinkBase + url.PathEscape(id)
}

// DeployLinkMarkdownSnippet returns the Markdown that shows the deploy button
// for a link.
func DeployLinkMarkdownSnippet(id string) string {
	return fmt.Sprintf("[![%s](%s)](%s)", deployButtonAlt, deployButtonURL, DeployLinkURL(id))
}

// DeployLinkHTMLSnippet returns the HTML that shows the deploy button for a
// link.
func DeployLinkHTMLSnippet(id string) string {
	return fmt.Sprintf("<a href=%q><img src=%q alt=%q></a>", DeployLinkURL(id), deployButtonURL, deployButtonAlt)
}

// DeployLinks renders a project's deploy links.
func DeployLinks(w io.Writer, links []apiclient.DeployLink) {
	if len(links) == 0 {
		fmt.Fprintln(w, "No deploy links found")
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 92, "%-14s  %-28s  %-14s  %-12s  %s", "ID", "Repository", "Branch", "Status", "Created")
	for i := range links {
		link := &links[i]
		fmt.Fprintf(w, "%-14s  %-28s  %-14s  %s  %s\n",
			Truncate(theme.StripControl(link.Id), 14),
			Truncate(theme.StripControl(link.RepoFullName), 28),
			Truncate(theme.StripControl(link.Branch), 14),
			statusCell(string(link.Status), 12, on),
			FormatTimeAgo(link.CreatedAt),
		)
	}
	summary(w, on, "Showing %d deploy link(s)", len(links))
}

// DeployLink renders one deploy link.
func DeployLink(w io.Writer, link *apiclient.DeployLink) {
	if link == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", theme.StripControl(link.Id))
	kv(w, on, "URL", "%s", DeployLinkURL(theme.StripControl(link.Id)))
	kv(w, on, "Status", "%s", theme.Status(string(link.Status), on))
	if link.DisabledReason != nil {
		kv(w, on, "Disabled reason", "%s", string(*link.DisabledReason))
	}
	kv(w, on, "Repository", "%s", theme.StripControl(link.RepoFullName))
	kv(w, on, "Branch", "%s", theme.StripControl(link.Branch))
	if link.Path != "" {
		kv(w, on, "Path", "%s", theme.StripControl(link.Path))
	}
	if link.SuggestedName != "" {
		kv(w, on, "Suggested name", "%s", theme.StripControl(link.SuggestedName))
	}
	if link.LastValidatedAt != nil {
		kv(w, on, "Last verified", "%s", FormatTimestamp(*link.LastValidatedAt))
	}
	if link.LastValidatedCommit != nil && *link.LastValidatedCommit != "" {
		kv(w, on, "Verified commit", "%s", theme.StripControl(*link.LastValidatedCommit))
	}
	kv(w, on, "Created", "%s", FormatTimestamp(link.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(link.UpdatedAt))
}

// DeployLinkCreated renders a new deploy link with the address and the button
// snippets to paste into a README or a page.
func DeployLinkCreated(w io.Writer, link *apiclient.DeployLink) {
	if link == nil {
		return
	}
	id := theme.StripControl(link.Id)
	Success(w, "Created deploy link %s", id)
	fmt.Fprintln(w)
	DeployLink(w, link)

	on := theme.On(w)
	fmt.Fprintf(w, "\n%s\n  %s\n", theme.Title("Markdown", on), DeployLinkMarkdownSnippet(id))
	fmt.Fprintf(w, "\n%s\n  %s\n", theme.Title("HTML", on), DeployLinkHTMLSnippet(id))
}

// DeployLinkStats renders a link's click and deploy counts: totals per kind,
// then the most recent days.
func DeployLinkStats(w io.Writer, linkID string, stats *apiclient.DeployLinkStats) {
	if stats == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Link", "%s", theme.StripControl(linkID))
	kv(w, on, "Since", "%s", FormatTimestamp(stats.Since))

	if len(stats.Totals) == 0 && len(stats.Daily) == 0 {
		fmt.Fprintln(w, "\nNo activity in this window")
		return
	}

	totals := map[apiclient.DeployLinkKindCountKind]apiclient.DeployLinkKindCount{}
	for _, total := range stats.Totals {
		totals[total.Kind] = total
	}
	tableHead(w, on, true, 36, "%-12s  %8s  %8s", "Kind", "Events", "Visitors")
	for _, kind := range deployLinkKinds {
		total, ok := totals[kind]
		if !ok {
			continue
		}
		fmt.Fprintf(w, "%-12s  %8d  %8d\n", kind, total.Events, total.Visitors)
	}

	if len(stats.Daily) > 0 {
		deployLinkDaily(w, on, stats.Daily)
	}
}

// deployLinkDaily prints one row per day, newest first, with a column for each
// kind. Days with no events are absent from the API's series, so they are
// absent here too.
func deployLinkDaily(w io.Writer, on bool, daily []apiclient.DeployLinkDailyCount) {
	type dayRow struct {
		day    string
		counts map[apiclient.DeployLinkDailyCountKind]int64
	}
	rows := map[string]*dayRow{}
	for _, entry := range daily {
		day := entry.Day.Format("2006-01-02")
		row, ok := rows[day]
		if !ok {
			row = &dayRow{day: day, counts: map[apiclient.DeployLinkDailyCountKind]int64{}}
			rows[day] = row
		}
		row.counts[entry.Kind] += entry.Events
	}
	days := make([]string, 0, len(rows))
	for day := range rows {
		days = append(days, day)
	}
	slices.Sort(days)
	slices.Reverse(days)
	shown := days
	if len(shown) > statsDailyMaxRow {
		shown = shown[:statsDailyMaxRow]
	}

	header := []any{"Day"}
	for _, kind := range deployLinkKinds {
		header = append(header, string(kind))
	}
	tableHead(w, on, true, 10+len(deployLinkKinds)*10, "%-10s"+strings.Repeat("  %8s", len(deployLinkKinds)), header...)
	for _, day := range shown {
		cells := []any{day}
		for _, kind := range deployLinkKinds {
			cells = append(cells, rows[day].counts[apiclient.DeployLinkDailyCountKind(kind)])
		}
		fmt.Fprintf(w, "%-10s"+strings.Repeat("  %8d", len(deployLinkKinds))+"\n", cells...)
	}
	if len(days) > len(shown) {
		summary(w, on, "Showing the latest %d of %d days; --json has every day", len(shown), len(days))
	}
}

// shortCommitLength is how many characters of a commit are shown where the full
// hash would crowd the line.
const shortCommitLength = 7

// ShortCommit shortens a commit hash for display.
func ShortCommit(sha string) string {
	sha = theme.StripControl(sha)
	if len(sha) > shortCommitLength {
		return sha[:shortCommitLength]
	}
	return sha
}

// TemplateSource names where a project installed from a deploy link was read
// from, as owner/name@shortsha.
func TemplateSource(source *apiclient.ProjectTemplateInstallationSource) string {
	return theme.StripControl(source.Repository) + "@" + ShortCommit(source.CommitSha)
}

// DeployLinkPreview renders what deploying a link would create, with the exact
// commit it is pinned to.
func DeployLinkPreview(w io.Writer, preview *apiclient.DeployLinkPreview) {
	if preview == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Deploy link", "%s", theme.StripControl(preview.Id))
	kv(w, on, "Repository", "%s (%s)", theme.StripControl(preview.Repository.FullName), theme.StripControl(preview.Repository.Url))
	kv(w, on, "Branch", "%s", theme.StripControl(preview.Branch))
	kv(w, on, "Commit", "%s", theme.StripControl(preview.CommitSha))
	if preview.Path != "" {
		kv(w, on, "Path", "%s", theme.StripControl(preview.Path))
	}
	kv(w, on, "Frontends", "%s", nameList(preview.Frontends))
	kv(w, on, "Functions", "%s", nameList(preview.Functions))
	fmt.Fprintf(w, "\n%s\n", theme.Warn("Volcano has not reviewed this code. Deploy only repositories you trust.", on))
}

func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	cleaned := make([]string, len(names))
	for i, name := range names {
		cleaned[i] = theme.StripControl(name)
	}
	return strings.Join(cleaned, ", ")
}
