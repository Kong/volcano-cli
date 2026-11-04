package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// Markers in the Size column of a directory the listing left incomplete. A
// directory's size is otherwise "-", so neither reads as a size.
const (
	unreadableMarker = "unreadable"
	incompleteMarker = "incomplete"
)

// SandboxFiles renders a Sandbox workspace listing in the API's order. Path is
// the last column, so a long path never moves the others. A directory the
// listing could not enter, or whose names it could not all carry, is marked in
// the Size column. What the listing is missing goes to warnings, after the
// table, where a long listing cannot scroll it away.
func SandboxFiles(w, warnings io.Writer, listing *apiclient.SandboxFileListResult, withSHA256 bool) {
	defer sandboxFilesIncomplete(warnings, listing)
	entries := listing.Entries
	if len(entries) == 0 {
		fmt.Fprintln(w, "No entries")
		return
	}
	on := theme.On(w)
	if withSHA256 {
		tableHead(w, on, false, 101, "%-9s  %-4s  %12s  %-64s  %s", "Type", "Mode", "Size", "SHA-256", "Path")
	} else {
		tableHead(w, on, false, 35, "%-9s  %-4s  %12s  %s", "Type", "Mode", "Size", "Path")
	}
	for _, entry := range entries {
		kind := sandboxText(string(entry.Type))
		size := sandboxSize(entry, on)
		mode := fmt.Sprintf("%04o", entry.Mode)
		path := sandboxText(entry.Path)
		if !withSHA256 {
			fmt.Fprintf(w, "%-9s  %-4s  %s  %s\n", kind, mode, size, path)
			continue
		}
		digest := "-"
		switch {
		case entry.Sha256 != nil:
			digest = sandboxText(*entry.Sha256)
		case entry.Sha256Skipped != nil:
			digest = "skipped: " + sandboxText(string(*entry.Sha256Skipped))
		}
		fmt.Fprintf(w, "%-9s  %-4s  %s  %-64s  %s\n", kind, mode, size, digest, path)
	}
	noun := "entries"
	if len(entries) == 1 {
		noun = "entry"
	}
	summary(w, on, "Total: %d %s", len(entries), noun)
}

// sandboxSize is an entry's Size cell: a regular file's size, a marker on a
// directory the listing left incomplete, or "-". It pads before coloring, so
// ANSI never counts toward the column width.
func sandboxSize(entry apiclient.SandboxFileEntry, on bool) string {
	switch {
	case entry.Type == apiclient.File:
		return fmt.Sprintf("%12d", entry.Size)
	case entry.Unreadable != nil && *entry.Unreadable:
		return theme.Warn(fmt.Sprintf("%12s", unreadableMarker), on)
	case entry.UnsupportedNames != nil && *entry.UnsupportedNames > 0:
		return theme.Warn(fmt.Sprintf("%12s", incompleteMarker), on)
	}
	return fmt.Sprintf("%12s", "-")
}

// sandboxFilesIncomplete warns when a listing is missing entries: directories
// the session user cannot list, and names no entry can carry, in the listed
// directory or in the directories marked incomplete.
func sandboxFilesIncomplete(w io.Writer, listing *apiclient.SandboxFileListResult) {
	var unreadable, incomplete, inside int
	for _, entry := range listing.Entries {
		if entry.Unreadable != nil && *entry.Unreadable {
			unreadable++
		}
		if entry.UnsupportedNames != nil && *entry.UnsupportedNames > 0 {
			incomplete++
			inside += *entry.UnsupportedNames
		}
	}
	top := 0
	if listing.UnsupportedNames != nil && *listing.UnsupportedNames > 0 {
		top = *listing.UnsupportedNames
	}
	if unreadable == 0 && top == 0 && inside == 0 {
		return
	}
	fmt.Fprintln(w)
	Warning(w, "The listing is incomplete.")
	if unreadable > 0 {
		verb, pronoun := "was", "it"
		if unreadable > 1 {
			verb, pronoun = "were", "them"
		}
		fmt.Fprintf(w, "  %s marked %s %s not entered: the session user lacks read or execute permission on %s.\n",
			countNoun(unreadable, "directory", "directories"), unreadableMarker, verb, pronoun)
	}
	if skipped := top + inside; skipped > 0 {
		var where []string
		if top > 0 {
			where = append(where, fmt.Sprintf("%d in the listed directory", top))
		}
		if inside > 0 {
			where = append(where, fmt.Sprintf("%d in %s marked %s", inside, countNoun(incomplete, "directory", "directories"), incompleteMarker))
		}
		reason := "was skipped, with everything below it, because it is not UTF-8 or its path"
		if skipped > 1 {
			reason = "were skipped, with everything below them, because they are not UTF-8 or their paths"
		}
		fmt.Fprintf(w, "  %s %s would pass 4096 bytes (%s).\n", countNoun(skipped, "name", "names"), reason, strings.Join(where, ", "))
	}
}

func countNoun(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// sandboxText prints a string from the listing as it is, or as a quoted Go
// string when it holds a quote, a backslash, or a character that is not
// printable, such as an escape, a newline, or a tab. A program in the Sandbox
// names its files, so a name must not move the cursor, change colors, or break
// the table. The table prints every string the server sends through it: the
// path, the type, the digest, and a digest's skip reason.
func sandboxText(text string) string {
	quoted := strconv.Quote(text)
	if quoted[1:len(quoted)-1] == text {
		return text
	}
	return quoted
}
