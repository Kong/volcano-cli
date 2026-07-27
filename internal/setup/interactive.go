package setup

import "fmt"

// Detected is one harness present on the machine, with whether Volcano is
// already installed into it and (for version-bearing harnesses) the installed
// and locally-known-latest versions, so an interactive picker can label each
// option with the same status vocabulary the non-interactive report uses.
type Detected struct {
	Name             string
	Installed        bool
	InstalledVersion string // "" when unknown or versionless (file-drop harnesses)
	LatestVersion    string // locally-cached latest; "" when unknown or versionless
}

// Updatable reports whether Volcano is installed but behind the locally-known
// latest version. Best-effort: the "latest" comes from the harness's cached
// marketplace snapshot, which can itself be stale, so this never reports a
// false positive (unparseable/ahead versions compare as not-updatable) but may
// miss an update newer than the last local refresh.
func (d Detected) Updatable() bool {
	return d.Installed && semverLess(d.InstalledVersion, d.LatestVersion)
}

// StatusMark returns the picker's install-state mark for a detected harness,
// padded to a fixed width so options align. The picker describes install *state*
// (pick what to set up): [available] not installed, [installed] current,
// [outdated] installed but behind. This differs from the report's outcome marks
// ([installed] = "installed this run", [detected] = "install failed"). The caller
// colors the mark and leaves the harness name in the terminal's default
// foreground.
func (d Detected) StatusMark() string {
	switch {
	case !d.Installed:
		return markPad("[available]")
	case d.Updatable():
		return markPad("[outdated]")
	default:
		return markPad("[installed]")
	}
}

// VersionNote is the trailing "(0.2.14 → 0.2.16 available)" hint appended to an
// outdated harness's picker label, or "" when there is nothing to add.
func (d Detected) VersionNote() string {
	if !d.Updatable() {
		return ""
	}
	return fmt.Sprintf(" (%s \u2192 %s available)", d.InstalledVersion, d.LatestVersion)
}

// markPad left-justifies a picker mark to a fixed width so the harness names
// line up regardless of which mark a row carries.
func markPad(mark string) string {
	return fmt.Sprintf("%-11s", mark)
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
		d := Detected{
			Name:      h.name,
			Installed: h.installed != nil && h.installed(env),
		}
		if h.version != nil {
			d.InstalledVersion, d.LatestVersion = h.version(env)
		}
		found = append(found, d)
	}
	return found, nil
}
