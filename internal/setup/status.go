package setup

import "path/filepath"

// PluginStatus reports whether Volcano is installed into the named harness and,
// for version-bearing harnesses, the installed version ("" when versionless or
// unknown). Unlike Detect it skips the harness presence probe: the caller
// already knows the harness is running. A name setup does not manage reports
// the manual ~/.volcano install instead. It reads local files only — no
// network, no install.
func PluginStatus(opts Options, name string) (installed bool, version string) {
	_, env, err := opts.resolve()
	if err != nil {
		return false, ""
	}
	for _, h := range harnesses() {
		if h.name != name {
			continue
		}
		if h.installed == nil || !h.installed(env) {
			return false, ""
		}
		if h.version != nil {
			version, _ = h.version(env)
		}
		return true, version
	}
	return dirHasVolcanoSkill(filepath.Join(env.home, ".volcano", "skills")), ""
}
