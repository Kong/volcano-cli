package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Variables renders one variable list page.
func Variables(w io.Writer, page *apiclient.PaginatedVariables, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedVariables{}
	}

	variables := page.Data
	if len(variables) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No variables configured")
		} else {
			fmt.Fprintf(w, "No variables found on page %d\n", page.Page)
		}
		printVariablePageSummary(w, page)
		return
	}

	fmt.Fprintf(w, "\n%-25s  %-15s  %-15s  %-15s\n", "Name", "Status", "Created", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 78))
	for _, variable := range variables {
		fmt.Fprintf(w, "%-25s  %-15s  %-15s  %-15s\n",
			Truncate(variable.Name, 25),
			variableStatus(variable),
			FormatTimeAgo(variable.CreatedAt),
			FormatTimeAgo(variable.UpdatedAt),
		)
	}
	printVariablePageSummary(w, page)
	if page.HasMore {
		fmt.Fprintf(w, "\nNext page: %s variables list --page %d --limit %d\n", commandPathPrefix(commandPrefix), page.Page+1, page.Limit)
	}
}

func printVariablePageSummary(w io.Writer, page *apiclient.PaginatedVariables) {
	fmt.Fprintf(w, "\nShowing %d of %d variable(s) (page %d, limit %d)\n", len(page.Data), page.Total, page.Page, page.Limit)
}

// Variable renders one project variable.
func Variable(w io.Writer, variable *apiclient.Variable) {
	fmt.Fprintf(w, "ID: %s\n", variable.Id.String())
	fmt.Fprintf(w, "Name: %s\n", variable.Name)
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(variable.CreatedAt))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(variable.UpdatedAt))
}

func variableStatus(variable apiclient.Variable) string {
	if variable.Status == nil {
		return "-"
	}
	status := strings.TrimSpace(string(*variable.Status))
	if status == "" {
		return "-"
	}
	return status
}
