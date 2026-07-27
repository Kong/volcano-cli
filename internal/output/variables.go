package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Variables renders one variable list page.
func Variables(w io.Writer, page *apiclient.PaginatedVariables, commandPrefix ...string) {
	if page == nil {
		page = &apiclient.PaginatedVariables{}
	}

	on := theme.On(w)
	variables := page.Data
	if len(variables) == 0 {
		if page.Total == 0 {
			fmt.Fprintln(w, "No variables configured")
		} else {
			fmt.Fprintf(w, "No variables found on page %d\n", page.Page)
		}
		printVariablePageSummary(w, on, page)
		return
	}

	tableHead(w, on, true, 78, "%-25s  %-15s  %-15s  %-15s", "Name", "Status", "Created", "Updated")
	for _, variable := range variables {
		fmt.Fprintf(w, "%-25s  %s  %-15s  %-15s\n",
			Truncate(variable.Name, 25),
			statusCell(variableStatus(variable), 15, on),
			FormatTimeAgo(variable.CreatedAt),
			FormatTimeAgo(variable.UpdatedAt),
		)
	}
	printVariablePageSummary(w, on, page)
	if page.HasMore {
		nextPage(w, on, fmt.Sprintf("%s variables list --page %d --limit %d", commandPathPrefix(commandPrefix), page.Page+1, page.Limit))
	}
}

func printVariablePageSummary(w io.Writer, on bool, page *apiclient.PaginatedVariables) {
	summary(w, on, "Showing %d of %d variable(s) (page %d, limit %d)", len(page.Data), page.Total, page.Page, page.Limit)
}

// Variable renders one project variable.
func Variable(w io.Writer, variable *apiclient.Variable) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", variable.Id.String())
	kv(w, on, "Name", "%s", variable.Name)
	kv(w, on, "Created", "%s", FormatTimestamp(variable.CreatedAt))
	kv(w, on, "Updated", "%s", FormatTimestamp(variable.UpdatedAt))
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
