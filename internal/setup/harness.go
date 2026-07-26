package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// CommandRunner runs an external command (used for marketplace-based installs).
// Production uses the package's execRunner; tests inject a fake. runtime.Deps has
// no general-purpose runner to reuse (only Local/Update/Git), so setup owns its
// default rather than wiring one through the CLI.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// marketplaceRepo is the GitHub marketplace source every plugin-capable harness
// installs from.
const marketplaceRepo = "Kong/volcano-agentic-plugins"

// marketplaceName is the marketplace's own identifier (the repo basename) as it
// appears in a harness's plugin registry. Used both as the "already installed"
// probe and to scope the per-harness "refresh this marketplace" commands so a
// rerun pulls the latest plugin version instead of the stale pinned snapshot.
const marketplaceName = "volcano-agentic-plugins"

// pluginRef is the marketplace plugin identifier to install.
const pluginRef = "volcano@volcano-agentic-plugins"

// environ holds the (injectable) view of the machine used for detection and
// path resolution, so tests can point it at a temp HOME.
type environ struct {
	home     string
	getenv   func(string) string
	lookPath func(string) (string, error)
}

// configHome resolves the XDG config base (~/.config unless XDG_CONFIG_HOME is
// set). opencode always uses this path regardless of OS, so we compute it
// directly rather than via os.UserConfigDir (which differs on macOS).
func (e environ) configHome() string {
	if x := strings.TrimSpace(e.getenv("XDG_CONFIG_HOME")); x != "" {
		return x
	}
	return filepath.Join(e.home, ".config")
}

// harness is one setup target: how to detect it, whether Volcano is already
// installed into it, and how to install. installed is a best-effort probe of the
// harness's own registry/skills dir so an interactive picker can show
// [installed] vs [available]; it never blocks install. version, set only for
// version-bearing (marketplace) harnesses, reports the installed and
// locally-known-latest Volcano version from local files (no network) so setup
// can distinguish install / update / up-to-date; nil for versionless (file-drop)
// harnesses.
type harness struct {
	name      string
	detect    func(e environ) bool
	installed func(e environ) bool
	version   func(e environ) (installed, available string)
	install   func(ctx context.Context, e environ, res resolved) (detail string, err error)
}

// order matters: marketplace harnesses first, then skills-drop harnesses. This
// is the display order in the report and the phased rollout order.
func harnesses() []harness {
	// Skills-drop directories, shared between the install and installed probes so
	// the two never disagree about where a harness's skills live.
	cursorSkills := func(e environ) string { return filepath.Join(e.home, ".cursor", "skills") }
	opencodeSkills := func(e environ) string { return filepath.Join(e.configHome(), "opencode", "skills") }
	piSkills := func(e environ) string { return filepath.Join(e.home, ".pi", "agent", "skills") }

	return []harness{
		{
			name:   "claude-code",
			detect: func(e environ) bool { return onPath(e, "claude") },
			installed: func(e environ) bool {
				return fileContains(filepath.Join(e.home, ".claude", "plugins", "installed_plugins.json"), marketplaceName)
			},
			version: claudeVersions,
			// The marketplace is a pinned snapshot, so a rerun installs the stale
			// version unless we refresh it first. `marketplace update` re-fetches the
			// source; `install` no-ops when already present; `update` then bumps the
			// installed plugin to the refreshed latest (restart required to apply).
			install: marketplaceInstall("claude", [][]string{
				{"plugin", "marketplace", "add", marketplaceRepo},
				{"plugin", "marketplace", "update", marketplaceName},
				{"plugin", "install", pluginRef},
				{"plugin", "update", pluginRef},
			}),
		},
		{
			name:   "codex",
			detect: func(e environ) bool { return onPath(e, "codex") },
			installed: func(e environ) bool {
				return dirExists(filepath.Join(e.home, ".codex", "plugins", "cache", marketplaceName))
			},
			version: codexVersions,
			// Codex uses `plugin add` (not `install`) and pins the marketplace to a
			// ref when added from GitHub (per plugins/codex/README.md). Codex has no
			// per-plugin update command, but `add` is idempotent and installs the
			// latest snapshot version, so `marketplace upgrade` before it makes a
			// rerun update the plugin.
			install: marketplaceInstall("codex", [][]string{
				{"plugin", "marketplace", "add", marketplaceRepo, "--ref", "main"},
				{"plugin", "marketplace", "upgrade", marketplaceName},
				{"plugin", "add", pluginRef},
			}),
		},
		{
			name:      "cursor",
			detect:    func(e environ) bool { return dirExists(filepath.Join(e.home, ".cursor")) },
			installed: skillsInstalled(cursorSkills),
			install:   skillsInstall(cursorSkills, nil),
		},
		{
			name:   "opencode",
			detect: func(e environ) bool { return dirExists(filepath.Join(e.configHome(), "opencode")) },
			// Skills only: ~/.config/opencode/AGENTS.md is user-owned, so we must not
			// overwrite it. opencode auto-discovers the dropped skills. A native
			// opencode plugin that wires AGENTS.md safely is tracked in VOL-511.
			installed: skillsInstalled(opencodeSkills),
			install:   skillsInstall(opencodeSkills, nil),
		},
		{
			name:      "pi",
			detect:    func(e environ) bool { return dirExists(filepath.Join(e.home, ".pi", "agent")) },
			installed: skillsInstalled(piSkills),
			install:   skillsInstall(piSkills, nil),
		},
	}
}

// skillsInstalled reports whether a Volcano skill folder already lives in the
// harness's skills directory — the "already installed" signal for file-drop
// harnesses.
func skillsInstalled(skillsDir func(environ) string) func(environ) bool {
	return func(e environ) bool { return dirHasVolcanoSkill(skillsDir(e)) }
}

// dirHasVolcanoSkill reports whether dir contains a subdirectory whose name
// mentions "volcano" (every Volcano skill folder does, e.g. volcano-platform,
// install-volcano), so user-owned unrelated skills don't false-positive.
func dirHasVolcanoSkill(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if !strings.Contains(strings.ToLower(ent.Name()), "volcano") {
			continue
		}
		// A skill entry may be a real directory or a symlink to one (some agents
		// link a shared skills repo). os.Stat follows the link, so symlinked
		// skills aren't missed the way DirEntry.IsDir() would miss them.
		if info, statErr := os.Stat(filepath.Join(dir, ent.Name())); statErr == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// claudeVersions reads claude-code's installed and locally-cached-latest Volcano
// plugin versions, both from local files (no network). Either may be "" when the
// file is absent or unparseable.
func claudeVersions(e environ) (installed, available string) {
	installed = claudeInstalledVersion(e)
	available = manifestVersion(filepath.Join(e.home, ".claude", "plugins", "marketplaces", marketplaceName, ".release-please-manifest.json"))
	return installed, available
}

// claudeInstalledVersion extracts the Volcano plugin's version from claude-code's
// authoritative registry (installed_plugins.json), which carries an explicit
// version field per installed plugin.
func claudeInstalledVersion(e environ) string {
	b, err := os.ReadFile(filepath.Join(e.home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Plugins map[string][]struct {
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	if entries := doc.Plugins[pluginRef]; len(entries) > 0 {
		return entries[0].Version
	}
	return ""
}

// codexVersions reads codex's installed and locally-cached-latest Volcano plugin
// versions from local files (no network).
func codexVersions(e environ) (installed, available string) {
	installed = codexInstalledVersion(e)
	available = manifestVersion(filepath.Join(e.home, ".codex", ".tmp", "marketplaces", marketplaceName, ".release-please-manifest.json"))
	return installed, available
}

// codexInstalledVersion returns the greatest version subdirectory under codex's
// plugin cache. Codex names each install dir by version and normally keeps only
// the active one, but a stale dir can linger, so pick the highest valid semver.
func codexInstalledVersion(e environ) string {
	dir := filepath.Join(e.home, ".codex", "plugins", "cache", marketplaceName, "volcano")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		if _, err := semver.NewVersion(ent.Name()); err != nil {
			continue // skip non-version dirs
		}
		if best == "" || semverLess(best, ent.Name()) {
			best = ent.Name()
		}
	}
	return best
}

// manifestVersion reads {".": "x.y.z"} from a release-please manifest — the
// version of the marketplace's root package, which is the Volcano plugin.
func manifestVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	return m["."]
}

// semverLess reports whether version a is strictly older than b. Unparseable
// versions compare as not-less, so a bad read never yields a false "outdated".
func semverLess(a, b string) bool {
	av, err1 := semver.NewVersion(a)
	bv, err2 := semver.NewVersion(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return av.LessThan(bv)
}

// fileContains reports whether the file at path exists and contains substr
// (case-insensitive). Used to read a marketplace harness's own plugin registry
// as the "already installed" signal.
func fileContains(path, substr string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(b)), strings.ToLower(substr))
}

const manualHarness = "manual"

// marketplaceInstall shells out to a harness's own non-interactive plugin
// commands, running each argv (after bin) in sequence. Preferred over file-drop
// wherever a harness provides such commands so the plugin registers in that
// harness's own plugin registry.
//
// `volcano setup` is expected to be re-run, so a command that fails only because
// the marketplace/plugin is already registered is treated as a no-op success:
// claude and codex share no exit-code contract for "already added", so their
// output text is the only cross-harness signal. Each sequence also refreshes its
// pinned marketplace snapshot and updates the plugin, so a rerun lands on the
// latest version; the caller (install) reads the resulting version and appends
// restartNote, since both harnesses load the new version only after a restart.
func marketplaceInstall(bin string, cmds [][]string) func(context.Context, environ, resolved) (string, error) {
	return func(ctx context.Context, _ environ, res resolved) (string, error) {
		for _, args := range cmds {
			if out, err := res.runner.Run(ctx, bin, args...); err != nil && !alreadyPresent(out) {
				return "", fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
		}
		return "marketplace: " + pluginRef, nil
	}
}

// restartNote is appended to a marketplace harness's detail when the plugin was
// installed or updated: both claude and codex load the new version only after
// the agent restarts.
const restartNote = " (restart your agent to apply)"

// alreadyPresent reports whether a failed plugin/marketplace command failed only
// because *our* plugin/marketplace was already registered — a no-op on rerun,
// not a real error. It requires both a known "already …" phrase and a mention of
// our own marketplace/plugin ("volcano"), so a genuine failure on the terminal
// install command (which has no following step to catch it) isn't masked just
// because its output happens to contain a generic phrase like a filesystem
// "destination directory already exists".
//
// ponytail: substring heuristic. An "already added from a different source"
// conflict still names our marketplace and would be tolerated; if that surfaces,
// match the exact per-harness rerun phrasing instead.
func alreadyPresent(out []byte) bool {
	s := strings.ToLower(string(out))
	if !strings.Contains(s, "volcano") { // pluginRef/marketplaceRepo both contain it
		return false
	}
	for _, phrase := range []string{"already added", "already installed", "already exists", "already present", "already registered"} {
		if strings.Contains(s, phrase) {
			return true
		}
	}
	return false
}

// skillsInstall file-drops skills into a harness's skills directory (and,
// optionally, AGENTS.md), used for harnesses without a non-interactive plugin
// command. skillsDir and agentsPath are computed from the environ at call time.
func skillsInstall(skillsDir, agentsPath func(environ) string) func(context.Context, environ, resolved) (string, error) {
	return func(ctx context.Context, e environ, res resolved) (string, error) {
		dir := skillsDir(e)
		var ap string
		if agentsPath != nil {
			ap = agentsPath(e)
		}
		n, err := materialize(ctx, res.doer, res.skillsBase, dir, ap)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d skills -> %s", n, dir), nil
	}
}

// manualInstall writes skills + AGENTS.md under ~/.volcano, the no-harness
// fallback (mirrors bootstrap.sh's manual target).
func manualInstall(ctx context.Context, e environ, res resolved) (string, error) {
	base := filepath.Join(e.home, ".volcano")
	skillsDir := filepath.Join(base, "skills")
	n, err := materialize(ctx, res.doer, res.skillsBase, skillsDir, filepath.Join(base, "AGENTS.md"))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d skills -> %s", n, skillsDir), nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func onPath(e environ, bin string) bool {
	_, err := e.lookPath(bin)
	return err == nil
}
