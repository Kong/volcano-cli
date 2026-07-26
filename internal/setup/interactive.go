package setup

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
