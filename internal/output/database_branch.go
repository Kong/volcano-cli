package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// DatabaseBranchCreated renders a branch that was just forked. The branch is
// still provisioning at this point, so there is no connection string to show.
func DatabaseBranchCreated(w io.Writer, branch *apiclient.DatabaseBranch, databaseName string, commandPrefix ...string) {
	on := theme.On(w)
	Success(w, "Branch '%s' of database '%s' created", branch.Name, databaseName)
	kv(w, on, "Status", "%s", theme.Status(branchStatus(*branch), on))
	kv(w, on, "Expires", "%s (%s)", FormatTimestamp(branch.ExpiresAt), formatBranchTTL(branch.TtlSeconds))
	fmt.Fprintf(w, "\n%s%s\n",
		theme.Dim("The branch is provisioning. Fetch it to get its connection string: ", on),
		theme.Command(fmt.Sprintf("%s databases branches get %s %s --show-connection-string",
			commandPathPrefix(commandPrefix), databaseName, branch.Name), on),
	)
}

// DatabaseBranches renders a database's branch list. The list endpoint omits
// connection strings, so there is nothing to reveal here.
func DatabaseBranches(w io.Writer, branches []apiclient.DatabaseBranch, databaseName string) {
	on := theme.On(w)
	if len(branches) == 0 {
		fmt.Fprintf(w, "No branches of database '%s'\n", databaseName)
		return
	}

	tableHead(w, on, false, 96, "%-20s  %-14s  %-12s  %-12s  %-15s", "Name", "Status", "Storage", "Expires in", "Created")
	for _, branch := range branches {
		fmt.Fprintf(w, "%-20s  %s  %-12s  %-12s  %-15s\n",
			Truncate(branch.Name, 20),
			statusCell(branchStatus(branch), 14, on),
			formatBranchStorage(branch.StorageBytes),
			formatBranchRemaining(branch.ExpiresAt),
			FormatTimeAgo(branch.CreatedAt),
		)
	}
	summary(w, on, "Showing %d branch(es) of database '%s'", len(branches), databaseName)
}

// DatabaseBranch renders one branch.
func DatabaseBranch(w io.Writer, branch *apiclient.DatabaseBranch, showConnectionString bool) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", branch.Id.String())
	kv(w, on, "Name", "%s", branch.Name)
	kv(w, on, "Status", "%s", theme.Status(branchStatus(*branch), on))
	kv(w, on, "Storage", "%s", formatBranchStorage(branch.StorageBytes))
	kv(w, on, "Expires", "%s", formatBranchDeadline(branch.ExpiresAt))
	kv(w, on, "Lifetime", "%s", formatBranchTTL(branch.TtlSeconds))
	if branch.LastInvokedAt != nil {
		kv(w, on, "Last used", "%s", FormatTimeAgo(*branch.LastInvokedAt))
	}
	if showConnectionString {
		printConnectionString(w, on, branch.ConnectionString, "branch")
	}
	kv(w, on, "Created", "%s", FormatTimestamp(branch.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(branch.UpdatedAt))
}

// DatabaseBranchRotatedConnectionString renders the connection string a
// password rotation returned. That string is the only copy of the new
// credential the user gets, so an absent one is called out rather than skipped.
func DatabaseBranchRotatedConnectionString(w io.Writer, branch *apiclient.DatabaseBranch, databaseName string, commandPrefix ...string) {
	on := theme.On(w)
	if connectionString := stringPtrValue(branch.ConnectionString); connectionString != "" {
		kv(w, on, "Connection string", "%s", connectionString)
		return
	}
	Warning(w, "the API returned no connection string for branch '%s'", branch.Name)
	fmt.Fprintf(w, "%s%s\n",
		theme.Dim("Read the branch's current string with: ", on),
		theme.Command(fmt.Sprintf("%s databases branches get %s %s --show-connection-string",
			commandPathPrefix(commandPrefix), databaseName, branch.Name), on),
	)
}

func branchStatus(branch apiclient.DatabaseBranch) string {
	status := strings.TrimSpace(string(branch.Status))
	if status == "" {
		return "-"
	}
	return status
}

// formatBranchStorage renders a branch's divergence from its parent. Branches
// report no size until the sampler has visited them.
func formatBranchStorage(storageBytes *int64) string {
	if storageBytes == nil {
		return "-"
	}
	return formatByteSize(*storageBytes)
}

func formatBranchTTL(ttlSeconds int64) string {
	return formatCompactDuration(time.Duration(ttlSeconds) * time.Second)
}

const branchExpiredLabel = "expired"

// formatBranchRemaining renders what is left of a lifetime, bare, for the list
// view's "Expires in" column. A deadline the API left out is absent rather than
// past, so it must not read as expired.
func formatBranchRemaining(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return "-"
	}
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		return branchExpiredLabel
	}
	return formatCompactDuration(remaining)
}

// formatBranchDeadline pairs the absolute deadline with what is left of it for
// the detail view.
func formatBranchDeadline(expiresAt time.Time) string {
	if expiresAt.IsZero() {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", FormatTimestamp(expiresAt), formatBranchExpiry(expiresAt))
}

// formatBranchExpiry renders the same figure for the detail view, where it sits
// beside an absolute timestamp and needs a preposition. "in" belongs only on a
// lifetime still running down, so it lives here rather than in the column value.
func formatBranchExpiry(expiresAt time.Time) string {
	if time.Until(expiresAt) <= 0 {
		return branchExpiredLabel
	}
	return "in " + formatBranchRemaining(expiresAt)
}

// formatCompactDuration renders a span as a single largest unit, for the
// lifetime and retention columns.
func formatCompactDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
