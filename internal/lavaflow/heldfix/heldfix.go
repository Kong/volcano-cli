// Package heldfix is a fix LavaFlow held for a person to open, as read from
// its run, and the marker lines Volcano writes in its diff. It depends only
// on the API's types, so the lavaflow service and the output renderer can
// both use it without depending on each other.
package heldfix

import (
	"strconv"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Fix is a fix LavaFlow held because a workflow in the repository could run
// it with the repository's secrets before a person approves it, as read for a
// person to open. Opening sends exactly its two fingerprints, as Volcano
// served them, so Volcano opens the diff and jobs the person was shown or
// nothing.
type Fix struct {
	Run *apiclient.LavaflowRun
	// DiffSHA256 is the SHA-256 of Run.Diff exactly as served.
	DiffSHA256 string
	// JobsSHA256 identifies every job in Run.HeldWorkflows and the commits
	// their workflows were read at.
	JobsSHA256 string
	// Withheld lists the files the diff leaves out because they may hold a
	// credential, by the path each marker quotes, or "" for one it doesn't
	// name.
	Withheld []string
	// Cut is a diff cut short, which doesn't show the whole fix.
	Cut bool
}

// Reviewable reports a diff that shows the whole fix. LavaFlow opens no
// other.
func (f *Fix) Reviewable() bool { return len(f.Withheld) == 0 && !f.Cut }

// withheldMarker starts the line Volcano writes in a diff in place of a file
// it withheld, before the file's quoted path.
const withheldMarker = "# LavaFlow withheld the changes to "

// WithheldLine reports a line Volcano writes in a diff in place of a file it
// withheld. No diff line starts with #, so every such line counts, even one
// worded unlike the marker.
func WithheldLine(line string) bool { return strings.HasPrefix(line, "#") }

// CutLine reports the line Volcano ends a diff it cut short with.
func CutLine(line string) bool { return strings.HasPrefix(line, "[diff truncated") }

// DiffNotShown lists the files a served diff marks as withheld, by the path
// each marker quotes, or "" for one that names none, and reports whether it
// marks a cut.
func DiffNotShown(diff string) (withheld []string, cut bool) {
	for line := range strings.SplitSeq(diff, "\n") {
		switch {
		case WithheldLine(line):
			withheld = append(withheld, withheldPath(line))
		case CutLine(line):
			cut = true
		}
	}
	return withheld, cut
}

// withheldPath is the path a withheld marker quotes, or "" for a marker that
// doesn't name one.
func withheldPath(line string) string {
	rest, ok := strings.CutPrefix(line, withheldMarker)
	if !ok {
		return ""
	}
	quoted, err := strconv.QuotedPrefix(rest)
	if err != nil {
		return ""
	}
	path, err := strconv.Unquote(quoted)
	if err != nil {
		return ""
	}
	return path
}
