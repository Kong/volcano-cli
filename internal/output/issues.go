package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// IssuesPage is one page of issues and the command for the next one.
type IssuesPage struct {
	Issues      []apiclient.Issue
	NextCommand string
}

// Issues renders one page of issues, most recently seen first.
func Issues(w io.Writer, page IssuesPage) {
	on := theme.On(w)
	if len(page.Issues) == 0 {
		fmt.Fprintln(w, "No issues")
		return
	}
	tableHead(w, on, false, 140, "%-36s  %-8s  %-24s  %-40s  %7s  %s",
		"ID", "Status", "Resource", "Title", "Events", "Last seen")
	for _, issue := range page.Issues {
		fmt.Fprintf(w, "%-36s  %s  %-24s  %-40s  %7d  %s\n",
			issue.Id.String(), statusCell(clean(string(issue.Status)), 8, on), Truncate(clean(issue.Resource.Name), 24),
			Truncate(clean(issue.Title), 40), issue.EventCount, formatUTC(issue.LastSeenAt))
	}
	summary(w, on, "Showing %s", plural(len(page.Issues), "issue", "issues"))
	if page.NextCommand != "" {
		nextPage(w, on, clean(page.NextCommand))
	}
}

// Issue renders one issue: what raised it, how often and when, and its
// status.
func Issue(w io.Writer, issue *apiclient.Issue) {
	on := theme.On(w)
	kv(w, on, "Issue", "%s", issue.Id)
	kv(w, on, "Title", "%s", clean(issue.Title))
	kv(w, on, "Status", "%s", theme.Status(clean(string(issue.Status)), on))
	kv(w, on, "Resource", "%s %s", lavaflowResourceType(string(issue.Resource.Type)), clean(issue.Resource.Name))
	if issue.ErrorType != nil {
		kv(w, on, "Error type", "%s", clean(*issue.ErrorType))
	}
	if issue.Culprit != nil {
		kv(w, on, "Culprit", "%s", clean(*issue.Culprit))
	}
	kv(w, on, "Exception", "%s", yesNo(issue.Exception))
	kv(w, on, "Events", "%d", issue.EventCount)
	kv(w, on, "First seen", "%s", formatUTC(issue.FirstSeenAt))
	kv(w, on, "Last seen", "%s", formatUTC(issue.LastSeenAt))
	if issue.LastRegion != nil {
		kv(w, on, "Last region", "%s", clean(*issue.LastRegion))
	}
	if issue.ResolvedAt != nil {
		kv(w, on, "Resolved", "%s", formatUTC(*issue.ResolvedAt))
	}
	if issue.RegressedAt != nil {
		kv(w, on, "Reopened", "%s", formatUTC(*issue.RegressedAt))
	}
}

// IssueNotesPage is one page of an issue's notes and the command for the
// next one.
type IssueNotesPage struct {
	Notes       []apiclient.IssueNote
	NextCommand string
}

// IssueNotes renders one page of notes, each with its text as written.
func IssueNotes(w io.Writer, page IssueNotesPage) {
	on := theme.On(w)
	if len(page.Notes) == 0 {
		fmt.Fprintln(w, "No notes")
		return
	}
	for i := range page.Notes {
		if i > 0 {
			fmt.Fprintln(w)
		}
		IssueNote(w, &page.Notes[i])
	}
	summary(w, on, "Showing %s", plural(len(page.Notes), "note", "notes"))
	if page.NextCommand != "" {
		nextPage(w, on, clean(page.NextCommand))
	}
}

// IssueNote renders one note: a heading line with what it is on, who wrote
// it and when, then its text, indented, line by line.
func IssueNote(w io.Writer, note *apiclient.IssueNote) {
	on := theme.On(w)
	target := "on the issue"
	if note.RunId != nil {
		target = "on run " + note.RunId.String()
	}
	heading := fmt.Sprintf("%s  %s  by %s  %s", theme.Title(note.Id.String(), on), target,
		noteAuthor(note.Author), formatUTC(note.CreatedAt))
	if note.Version > 1 {
		heading += theme.Dim("  edited "+formatUTC(note.UpdatedAt)+", version "+strconv.Itoa(note.Version), on)
	}
	fmt.Fprintln(w, heading)
	for line := range strings.SplitSeq(strings.TrimRight(note.Body, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", clean(line))
	}
}

// noteAuthor names a note's author by email when the reader may see it, by
// user ID otherwise.
func noteAuthor(author *apiclient.IssueNoteAuthor) string {
	switch {
	case author == nil:
		return "a deleted account"
	case author.Email != nil && *author.Email != "":
		return clean(*author.Email)
	default:
		return "user " + clean(author.Id)
	}
}

// ControlSafeJSON writes value as indented JSON, for a command's --json
// output, with DEL and the C1 controls, which encoding/json passes through,
// as \u escapes like the C0 controls it already escapes. They can only appear
// inside a JSON string, where the escape means the same.
func ControlSafeJSON(w io.Writer, value any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	var b strings.Builder
	for _, r := range buf.String() {
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "\\u%04x", r)
			continue
		}
		b.WriteRune(r)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// formatUTC prints a time to the minute in UTC, saying so, as the API's own
// times are.
func formatUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04") + " UTC"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
