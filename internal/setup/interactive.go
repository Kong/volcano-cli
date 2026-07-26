package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Detect returns the names of the coding-agent harnesses present on this
// machine, in the same order Run would install them. It performs detection
// only — no install, no network — so an interactive front-end can seed a
// selection prompt before anything is written.
func Detect(opts Options) ([]string, error) {
	_, env, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	var found []string
	for _, h := range harnesses() {
		if h.detect(env) {
			found = append(found, h.name)
		}
	}
	return found, nil
}

// SelectHarnesses prompts (on w) for which of the detected harnesses to set up
// and reads one line from r. It never loops, so it cannot hang beyond a single
// read: on EOF (closed/piped stdin) it returns all detected, so even a
// mis-detected non-TTY caller falls through to the default rather than blocking.
//
// Input handling (case-insensitive, trimmed):
//   - empty / "a" / "all" / "y" / "yes"  -> all detected
//   - "n" / "no" / "q" / "quit"          -> none (caller should cancel)
//   - "1,3" / "1 3"                       -> that subset (1-based, deduped)
//
// An out-of-range or non-numeric token is an error rather than a silent drop,
// so a fat-fingered selection fails loudly instead of installing the wrong set.
func SelectHarnesses(r io.Reader, w io.Writer, detected []string) ([]string, error) {
	fmt.Fprintln(w, "Detected coding agents:")
	for i, name := range detected {
		fmt.Fprintf(w, "  %d. %s\n", i+1, name)
	}
	fmt.Fprint(w, "\nInstall Volcano for which? [Enter = all, e.g. 1,3, or n to cancel]: ")

	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "a", "all", "y", "yes":
		return append([]string(nil), detected...), nil
	case "n", "no", "q", "quit":
		return nil, nil
	}

	fields := strings.FieldsFunc(strings.TrimSpace(line), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	var selected []string
	seen := make(map[int]bool, len(fields))
	for _, f := range fields {
		n, convErr := strconv.Atoi(f)
		if convErr != nil || n < 1 || n > len(detected) {
			return nil, fmt.Errorf("invalid selection %q (choose 1-%d)", f, len(detected))
		}
		if !seen[n] {
			seen[n] = true
			selected = append(selected, detected[n-1])
		}
	}
	return selected, nil
}
