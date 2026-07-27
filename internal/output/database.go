// Package output formats command results for human and machine consumers.
package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// DatabaseCreated renders a newly created database.
func DatabaseCreated(w io.Writer, database *apiclient.Database, showConnectionString bool) {
	on := theme.On(w)
	Success(w, "Database '%s' created", database.Name)
	kv(w, on, "Status", "%s", theme.Status(databaseStatus(*database), on))
	if showConnectionString {
		printConnectionString(w, on, database.ConnectionString)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(database.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(database.UpdatedAt))
}

// Databases renders one database list page.
func Databases(w io.Writer, page *apiclient.PaginatedDatabases, showConnectionString bool, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedDatabases{}
	}

	on := theme.On(w)
	databases := page.Data
	if len(databases) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No databases configured")
		} else {
			fmt.Fprintf(w, "No databases found on page %d\n", page.Page)
		}
		printDatabasePageSummary(w, on, page)
		return
	}

	tableHead(w, on, false, 96, "%-20s  %-12s  %-15s  %-8s  %-15s  %-15s", "Name", "Status", "Region", "PG", "Created", "Updated")
	for _, database := range databases {
		fmt.Fprintf(w, "%-20s  %s  %-15s  %-8s  %-15s  %-15s\n",
			Truncate(database.Name, 20),
			statusCell(databaseStatus(database), 12, on),
			Truncate(blankString(stringPtrValue(database.Region)), 15),
			blankString(stringPtrValue(database.PgVersion)),
			FormatTimeAgo(database.CreatedAt),
			FormatTimeAgo(database.UpdatedAt),
		)
		if showConnectionString {
			printConnectionString(w, on, database.ConnectionString)
		}
	}
	printDatabasePageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s databases list --page %d --limit %d", commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printDatabasePageSummary(w io.Writer, on bool, page *apiclient.PaginatedDatabases) {
	summary(w, on, "Showing %d of %d database(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

// Database renders one database.
func Database(w io.Writer, database *apiclient.Database, showConnectionString bool) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", database.Id.String())
	kv(w, on, "Name", "%s", database.Name)
	kv(w, on, "Status", "%s", theme.Status(databaseStatus(*database), on))
	if region := stringPtrValue(database.Region); region != "" {
		kv(w, on, "Region", "%s", region)
	}
	if pgVersion := stringPtrValue(database.PgVersion); pgVersion != "" {
		kv(w, on, "PostgreSQL version", "%s", pgVersion)
	}
	if database.DatabaseType != nil {
		kv(w, on, "Type", "%s", string(*database.DatabaseType))
	}
	if showConnectionString {
		printConnectionString(w, on, database.ConnectionString)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(database.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(database.UpdatedAt))
}

func printConnectionString(w io.Writer, on bool, value *string) {
	if connectionString := stringPtrValue(value); connectionString != "" {
		kv(w, on, "Connection string", "%s", connectionString)
	}
}

func databaseStatus(database apiclient.Database) string {
	status := strings.TrimSpace(string(database.Status))
	if status == "" {
		return "-"
	}
	return status
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func blankString(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
