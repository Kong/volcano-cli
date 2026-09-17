// Package confirm prompts the user for yes/no confirmation on stdin.
package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

const destructiveDeleteMessage = "You are about to delete a resource permanently, this may cause loss of data and/or service interruptions. Are you sure?"

// ErrCannotPrompt is returned instead of a decline when nothing can answer the
// prompt. Callers that want to say more about what went unconfirmed check
// CanPrompt themselves first and return their own error.
var ErrCannotPrompt = errors.New("stdin is not a terminal: confirmation required; pass --yes")

// CanPrompt reports whether there is a human on the other end of r.
//
// A prompt read from a closed or piped stdin comes back as a decline, so a
// command that only checks the answer exits 0 having done nothing — which is
// what an agent or CI job cannot tell apart from success. Delete and Action
// refuse outright rather than let that happen; this is exported for callers
// that want to explain the refusal in their own words.
//
// A reader that is not the process's stdin at all — an injected one, as in
// tests — is promptable, since something is deliberately feeding it answers.
func CanPrompt(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	return term.IsTerminal(f.Fd())
}

// Delete prompts for delete confirmation for one or more named resources. It
// returns ErrCannotPrompt when nothing can answer, rather than reporting the
// unanswered prompt as a decline.
func Delete(r io.Reader, w io.Writer, resource string, names ...string) (bool, error) {
	if !CanPrompt(r) {
		return false, ErrCannotPrompt
	}
	fmt.Fprintln(w, destructiveDeleteMessage)
	fmt.Fprint(w, deletePrompt(resource, names...))
	return readConfirmation(r, w, "Delete cancelled.")
}

// Action prompts for confirmation of a destructive change other than a delete.
// warning describes what the caller is about to do, and question asks it. Like
// Delete, an unanswerable prompt is an error rather than a decline.
func Action(r io.Reader, w io.Writer, warning, question string) (bool, error) {
	if !CanPrompt(r) {
		return false, ErrCannotPrompt
	}
	fmt.Fprintln(w, warning)
	fmt.Fprintf(w, "%s Type 'y' or 'yes' to confirm: ", question)
	return readConfirmation(r, w, "Cancelled.")
}

func deletePrompt(resource string, names ...string) string {
	if len(names) == 1 {
		return fmt.Sprintf("Delete %s '%s'? Type 'y' or 'yes' to confirm: ", resource, names[0])
	}

	quotedNames := make([]string, 0, len(names))
	for _, name := range names {
		quotedNames = append(quotedNames, fmt.Sprintf("'%s'", name))
	}
	return fmt.Sprintf("Delete %d %s: %s? Type 'y' or 'yes' to confirm: ", len(names), resource, strings.Join(quotedNames, ", "))
}

func readConfirmation(r io.Reader, w io.Writer, cancelMessage string) (bool, error) {
	input, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	normalized := strings.ToLower(strings.TrimSpace(input))
	confirmed := normalized == "y" || normalized == "yes"
	if !confirmed {
		fmt.Fprintln(w, cancelMessage)
	}
	return confirmed, nil
}
