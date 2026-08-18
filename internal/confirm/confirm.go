// Package confirm prompts the user for yes/no confirmation on stdin.
package confirm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const destructiveDeleteMessage = "You are about to delete a resource permanently, this may cause loss of data and/or service interruptions. Are you sure?"

// Delete prompts for delete confirmation for one or more named resources.
func Delete(r io.Reader, w io.Writer, resource string, names ...string) (bool, error) {
	fmt.Fprintln(w, destructiveDeleteMessage)
	fmt.Fprint(w, deletePrompt(resource, names...))
	return readConfirmation(r, w, "Delete cancelled.")
}

// Action prompts for confirmation of a destructive change other than a delete.
// warning describes what the caller is about to do, and question asks it.
func Action(r io.Reader, w io.Writer, warning, question string) (bool, error) {
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
