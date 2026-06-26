package output

import (
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/projectconfig"
)

// ProjectConfigDeploySummary renders the counts from a `config deploy` run.
func ProjectConfigDeploySummary(w io.Writer, summary *projectconfig.Summary) {
	if summary == nil {
		return
	}
	fmt.Fprintf(w, "Buckets: %d created, %d updated, %d unchanged\n",
		summary.BucketsCreated,
		summary.BucketsUpdated,
		summary.BucketsUnchanged,
	)
	fmt.Fprintf(w, "Policies: %d created, %d updated, %d deleted, %d unchanged\n",
		summary.PoliciesCreated,
		summary.PoliciesUpdated,
		summary.PoliciesDeleted,
		summary.PoliciesUnchanged,
	)
	fmt.Fprintf(w, "Functions: %d updated, %d unchanged\n",
		summary.FunctionsUpdated,
		summary.FunctionsUnchanged,
	)
	fmt.Fprintf(w, "Schedulers: %d created, %d updated, %d unchanged\n",
		summary.SchedulersCreated,
		summary.SchedulersUpdated,
		summary.SchedulersUnchanged,
	)
}
