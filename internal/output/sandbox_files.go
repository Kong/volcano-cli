package output

import (
	"fmt"
	"io"
	"strconv"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// SandboxFiles renders a Sandbox workspace listing in the API's order. Path is
// the last column, so a long path never moves the others.
func SandboxFiles(w io.Writer, entries []apiclient.SandboxFileEntry, withSHA256 bool) {
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
		size := "-"
		if entry.Type == apiclient.File {
			size = strconv.FormatInt(entry.Size, 10)
		}
		mode := fmt.Sprintf("%04o", entry.Mode)
		path := sandboxPath(entry.Path)
		if !withSHA256 {
			fmt.Fprintf(w, "%-9s  %-4s  %12s  %s\n", entry.Type, mode, size, path)
			continue
		}
		digest := "-"
		switch {
		case entry.Sha256 != nil:
			digest = *entry.Sha256
		case entry.Sha256Skipped != nil:
			digest = "skipped: " + sandboxPath(string(*entry.Sha256Skipped))
		}
		fmt.Fprintf(w, "%-9s  %-4s  %12s  %-64s  %s\n", entry.Type, mode, size, digest, path)
	}
	noun := "entries"
	if len(entries) == 1 {
		noun = "entry"
	}
	summary(w, on, "Total: %d %s", len(entries), noun)
}

// sandboxPath prints a workspace path as it is, or as a quoted Go string when
// it holds a quote, a backslash, or a character that is not printable, such as
// an escape, a newline, or a tab. A program in the Sandbox names its files, so
// a name must not move the cursor, change colors, or break the table. The table
// prints a digest's skip reason through it as well.
func sandboxPath(path string) string {
	quoted := strconv.Quote(path)
	if quoted[1:len(quoted)-1] == path {
		return path
	}
	return quoted
}
