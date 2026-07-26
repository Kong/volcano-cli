package setup

// Detected is one harness present on the machine, with whether Volcano is
// already installed into it, so an interactive picker can label each option
// with the same status vocabulary the non-interactive report uses.
type Detected struct {
	Name      string
	Installed bool
}

// StatusMark returns the picker's install-state mark for a detected harness. The
// picker describes install *state* (pick what to set up), so it uses
// [installed]/[available], describing install state rather than the report's
// outcome ([installed] there means "installed this run", and [detected] means
// "install failed" — not "not yet installed"). Both marks are 11 columns wide,
// so options align without padding. The caller colors the mark and leaves the
// harness name in the terminal's default foreground.
func (d Detected) StatusMark() string {
	if d.Installed {
		return "[installed]"
	}
	return "[available]"
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
