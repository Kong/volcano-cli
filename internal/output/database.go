// Package output formats command results for human and machine consumers.
package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// DatabaseCreated renders a newly created database.
func DatabaseCreated(w io.Writer, database *apiclient.Database, showConnectionString bool) {
	Success(w, "Database '%s' created", database.Name)
	fmt.Fprintf(w, "Status: %s\n", databaseStatus(*database))
	if showConnectionString {
		printConnectionString(w, database.ConnectionString)
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(database.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(database.UpdatedAt))
}

// Databases renders one database list page.
func Databases(w io.Writer, page *apiclient.PaginatedDatabases, showConnectionString bool, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedDatabases{}
	}

	databases := page.Data
	if len(databases) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No databases configured")
		} else {
			fmt.Fprintf(w, "No databases found on page %d\n", page.Page)
		}
		printDatabasePageSummary(w, page)
		return
	}

	fmt.Fprintf(w, "%-20s  %-12s  %-15s  %-8s  %-15s  %-15s\n", "Name", "Status", "Region", "PG", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 96))
	for _, database := range databases {
		fmt.Fprintf(w, "%-20s  %-12s  %-15s  %-8s  %-15s  %-15s\n",
			Truncate(database.Name, 20),
			databaseStatus(database),
			Truncate(blankString(stringPtrValue(database.Region)), 15),
			blankString(stringPtrValue(database.PgVersion)),
			FormatTimeAgo(database.CreatedAt),
			FormatTimeAgo(database.UpdatedAt),
		)
		if showConnectionString {
			printConnectionString(w, database.ConnectionString)
		}
	}
	printDatabasePageSummary(w, page)
	if page.HasMore {
		fmt.Fprintf(w, "\nNext page: %s databases list --page %d --limit %d\n", commandPathPrefix(commandPrefix), page.Page+1, page.Limit)
	}
}

func printDatabasePageSummary(w io.Writer, page *apiclient.PaginatedDatabases) {
	fmt.Fprintf(w, "\nShowing %d of %d database(s) (page %d, limit %d)\n", len(page.Data), page.Total, page.Page, page.Limit)
}

// Database renders one database.
func Database(w io.Writer, database *apiclient.Database, showConnectionString bool) {
	fmt.Fprintf(w, "ID: %s\n", database.Id.String())
	fmt.Fprintf(w, "Name: %s\n", database.Name)
	fmt.Fprintf(w, "Status: %s\n", databaseStatus(*database))
	if region := stringPtrValue(database.Region); region != "" {
		fmt.Fprintf(w, "Region: %s\n", region)
	}
	if pgVersion := stringPtrValue(database.PgVersion); pgVersion != "" {
		fmt.Fprintf(w, "PostgreSQL version: %s\n", pgVersion)
	}
	if database.DatabaseType != nil {
		fmt.Fprintf(w, "Type: %s\n", string(*database.DatabaseType))
	}
	if showConnectionString {
		printConnectionString(w, database.ConnectionString)
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(database.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(database.UpdatedAt))
}

func printConnectionString(w io.Writer, value *string) {
	if connectionString := stringPtrValue(value); connectionString != "" {
		fmt.Fprintf(w, "Connection string: %s\n", connectionString)
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
