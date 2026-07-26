package setup

import "fmt"

// Detected is one harness present on the machine, with whether Volcano is
// already installed into it, so an interactive picker can label each option
// with the same status vocabulary the non-interactive report uses.
type Detected struct {
	Name      string
	Installed bool
}

// Label renders the picker line for a detected harness using the report's own
// status marks: [ok] when Volcano is already installed, [detected] otherwise.
// Reusing statusMark keeps the interactive and non-interactive messaging in sync.
func (d Detected) Label() string {
	mark := statusMark(StatusDetected)
	if d.Installed {
		mark = statusMark(StatusInstalled)
	}
	return fmt.Sprintf("%-10s %s", mark, d.Name)
}

// Detect returns the coding-agent harnesses present on this machine, in the
// same order Run would install them, each flagged with whether Volcano is
// already installed. It performs detection only — no install, no network — so
// an interactive front-end can seed a selection prompt before anything is
// written.
func Detect(opts Options) ([]Detected, error) {
	_, env, err := opts.resolve()
	if err != nil {
		return nil, err
	}
	var found []Detected
	for _, h := range harnesses() {
		if !h.detect(env) {
			continue
		}
		found = append(found, Detected{
			Name:      h.name,
			Installed: h.installed != nil && h.installed(env),
		})
	}
	return found, nil
}
