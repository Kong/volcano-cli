package setup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommandRunner runs an external command (used for marketplace-based installs).
// It matches runtime.CommandRunner so the CLI's injected runner can be reused.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// marketplaceRepo is the GitHub marketplace source every plugin-capable harness
// installs from.
const marketplaceRepo = "Kong/volcano-agentic-plugins"

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

// harness is one setup target: how to detect it and how to install into it.
type harness struct {
	name    string
	detect  func(e environ) bool
	install func(ctx context.Context, e environ, res resolved) (detail string, err error)
}

// order matters: marketplace harnesses first, then skills-drop harnesses. This
// is the display order in the report and the phased rollout order.
func harnesses() []harness {
	return []harness{
		{
			name:    "claude-code",
			detect:  func(e environ) bool { return onPath(e, "claude") },
			install: marketplaceInstall("claude"),
		},
		{
			name:    "codex",
			detect:  func(e environ) bool { return onPath(e, "codex") },
			install: marketplaceInstall("codex"),
		},
		{
			name:   "cursor",
			detect: func(e environ) bool { return dirExists(filepath.Join(e.home, ".cursor")) },
			install: skillsInstall(
				func(e environ) string { return filepath.Join(e.home, ".cursor", "skills") },
				nil,
			),
		},
		{
			name:   "opencode",
			detect: func(e environ) bool { return dirExists(filepath.Join(e.configHome(), "opencode")) },
			install: skillsInstall(
				func(e environ) string { return filepath.Join(e.configHome(), "opencode", "skills") },
				func(e environ) string { return filepath.Join(e.configHome(), "opencode", "AGENTS.md") },
			),
		},
		{
			name:   "pi",
			detect: func(e environ) bool { return dirExists(filepath.Join(e.home, ".pi", "agent")) },
			install: skillsInstall(
				func(e environ) string { return filepath.Join(e.home, ".pi", "agent", "skills") },
				nil,
			),
		},
	}
}

const manualHarness = "manual"

// marketplaceInstall shells out to a harness's own non-interactive plugin
// commands. Preferred over file-drop wherever a harness provides them so the
// plugin registers in that harness's plugin registry.
func marketplaceInstall(bin string) func(context.Context, environ, resolved) (string, error) {
	return func(ctx context.Context, _ environ, res resolved) (string, error) {
		if out, err := res.runner.Run(ctx, bin, "plugin", "marketplace", "add", marketplaceRepo); err != nil {
			return "", fmt.Errorf("%s plugin marketplace add: %w: %s", bin, err, strings.TrimSpace(string(out)))
		}
		if out, err := res.runner.Run(ctx, bin, "plugin", "install", pluginRef); err != nil {
			return "", fmt.Errorf("%s plugin install: %w: %s", bin, err, strings.TrimSpace(string(out)))
		}
		return "marketplace: " + pluginRef, nil
	}
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
		n, err := materialize(ctx, res.doer, res.webURL, dir, ap)
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
	n, err := materialize(ctx, res.doer, res.webURL, skillsDir, filepath.Join(base, "AGENTS.md"))
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
