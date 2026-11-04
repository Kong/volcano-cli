package output

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/theme"
)

// GitRunsPage is one page of Git deployment runs, and the command that shows
// the next one when there is more.
type GitRunsPage struct {
	Runs        []api.GitDeploymentRun
	NextCommand string
	// GetCommand is the command that shows one run, such as
	// "volcano git runs get".
	GetCommand string
}

// GitRuns renders a page of Git deployment runs, newest push first. The
// Config column appears only when a run on the page reports what applying its
// volcano-config.yaml sections did.
func GitRuns(w io.Writer, page GitRunsPage) {
	on := theme.On(w)
	if len(page.Runs) == 0 {
		fmt.Fprintln(w, "No Git deployment runs. Each push to the production branch records one.")
		return
	}
	showConfig := slices.ContainsFunc(page.Runs, func(run api.GitDeploymentRun) bool {
		return gitRunConfigStatus(run.Config) != ""
	})
	configHead := ""
	if showConfig {
		configHead = fmt.Sprintf("%-10s  ", "Config")
	}
	tableHead(w, on, false, 120, "%-36s  %-10s  %-20s  %-7s  %-16s  %s%s",
		"ID", "Status", "Branch", "Commit", "Pushed", configHead, "Reason")
	for _, run := range page.Runs {
		reason := "-"
		if run.Error != nil {
			reason = clean(run.Error.Code)
		}
		config := ""
		if showConfig {
			config = fmt.Sprintf("%-10s  ", stringOrDash(gitRunConfigStatus(run.Config)))
		}
		fmt.Fprintf(w, "%-36s  %s  %-20s  %-7s  %-16s  %s%s\n",
			run.Id.String(),
			statusCell(string(run.Status), 10, on),
			Truncate(clean(run.Branch), 20),
			shortSHA(run.CommitSha),
			FormatTimeAgo(run.CreatedAt),
			config,
			reason)
	}
	summary(w, on, "Showing %d run(s)", len(page.Runs))
	if page.GetCommand != "" {
		fmt.Fprintf(w, "%s%s\n", theme.Dim("Show one with ", on), theme.Command(page.GetCommand+" <run-id>", on))
	}
	if page.NextCommand != "" {
		nextPage(w, on, clean(page.NextCommand))
	}
}

// GitRun renders one Git deployment run: the push, where the run got to, the
// migration runs it started, why it failed or was skipped in the API's own
// words, and what applying its volcano-config.yaml sections did.
func GitRun(w io.Writer, run *api.GitDeploymentRun) {
	on := theme.On(w)
	kv(w, on, "Run", "%s", run.Id)
	kv(w, on, "Status", "%s", theme.Status(string(run.Status), on))
	kv(w, on, "Branch", "%s", clean(run.Branch))
	kv(w, on, "Commit", "%s", clean(run.CommitSha))
	kv(w, on, "Pushed", "%s", formatLavaflowTime(run.CreatedAt))
	if run.StartedAt != nil {
		kv(w, on, "Started", "%s", formatLavaflowTime(*run.StartedAt))
	}
	if run.CompletedAt != nil {
		kv(w, on, "Finished", "%s", formatLavaflowTime(*run.CompletedAt))
	}
	kv(w, on, "Migrations", "%s", gitRunMigrations(run.ApplyMigrations))
	for _, migration := range run.MigrationRuns {
		kv(w, on, "Migration run", "%s on database %s", migration.Id, clean(migration.Database.Name))
	}
	gitRunConfig(w, on, run.Config)
	if run.Error != nil {
		// The API words each code itself, so the message is printed as given.
		message := clean(run.Error.Message)
		if message == "" {
			message = "no reason given"
		}
		fmt.Fprintln(w)
		Warning(w, "%s: %s", clean(run.Error.Code), message)
	}
}

func gitRunMigrations(apply bool) string {
	if apply {
		return "applied before deploying"
	}
	return "not applied (apply_migrations was off)"
}

func shortSHA(sha string) string {
	sha = clean(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func stringOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// gitRunConfigResult is what applying a push's opted-in volcano-config.yaml
// sections did, as the API reports it on a run. Nothing here is a value from
// the manifest: only sections, counts and codes.
type gitRunConfigResult struct {
	Status   string `json:"status"`
	Code     string `json:"code"`
	Sections []struct {
		Section   string         `json:"section"`
		Created   int            `json:"created"`
		Updated   int            `json:"updated"`
		Unchanged int            `json:"unchanged"`
		Failed    int            `json:"failed"`
		Held      map[string]int `json:"held"`
	} `json:"sections"`
}

// parseGitRunConfig reads a run's config result. ok is false when the run
// reports none, or reports one this CLI cannot read, which --json still shows
// whole.
func parseGitRunConfig(raw json.RawMessage) (result gitRunConfigResult, present, ok bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return gitRunConfigResult{}, false, false
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Status == "" {
		return gitRunConfigResult{}, true, false
	}
	return result, true, true
}

func gitRunConfigStatus(raw json.RawMessage) string {
	result, present, ok := parseGitRunConfig(raw)
	switch {
	case ok:
		return clean(result.Status)
	case present:
		return "reported"
	}
	return ""
}

// gitRunConfig renders what applying the run's volcano-config.yaml sections
// did, one line per section, and nothing for a run that reports none.
func gitRunConfig(w io.Writer, on bool, raw json.RawMessage) {
	result, present, ok := parseGitRunConfig(raw)
	switch {
	case !present:
		return
	case !ok:
		kv(w, on, "Config", "reported in a form this CLI can't read; see --json")
		return
	}
	status := clean(result.Status)
	if result.Code != "" {
		status += " (" + clean(result.Code) + ")"
	}
	kv(w, on, "Config", "%s", status)
	for _, section := range result.Sections {
		line := fmt.Sprintf("%d created, %d updated, %d unchanged, %d failed",
			section.Created, section.Updated, section.Unchanged, section.Failed)
		fmt.Fprintf(w, "  %s %s\n", theme.Dim(clean(section.Section)+":", on), line)
		for _, reason := range slices.Sorted(maps.Keys(section.Held)) {
			fmt.Fprintf(w, "    %s %d left for config deploy\n", theme.Dim(clean(reason)+":", on), section.Held[reason])
		}
	}
}
