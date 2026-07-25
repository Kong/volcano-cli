package update

import (
	"os"
	"path/filepath"
	"strings"
)

// InstallMethod identifies how the running CLI was installed, which decides how
// `volcano upgrade` upgrades it: delegate to the package manager (npm, brew, …)
// or replace the binary in place (script/manual installs).
type InstallMethod string

// Install method identifiers. InstallUnknown ("") means "not determined";
// callers treat it like a script/manual install (self-replace).
const (
	InstallNPM  InstallMethod = "npm"
	InstallPNPM InstallMethod = "pnpm"
	InstallYarn InstallMethod = "yarn"
	InstallBun  InstallMethod = "bun"
	InstallBrew InstallMethod = "brew"
	// InstallBrewStaging is a Homebrew install of the staging channel's
	// `volcano-staging` formula, which coexists with the production `volcano`
	// formula and must upgrade via `brew upgrade volcano-staging`.
	InstallBrewStaging InstallMethod = "brew-staging"
	InstallScript      InstallMethod = "script"
	InstallUnknown     InstallMethod = ""
)

// npmPackageName is the published npm package. Upgrading a JS-package-manager
// install re-installs this at @latest.
const npmPackageName = "@volcano.dev/cli"

// installMarkerName is the file each installer writes next to the binary to
// record how it was installed (scripts/npm/download.js, scripts/install-volcano.sh).
// It is the authoritative signal; path inference is only a fallback for installs
// that predate the marker.
const installMarkerName = ".volcano-install-method"

// DetectInstallMethod reports how the CLI at exePath was installed, preferring a
// marker file written at install time and falling back to the executable path.
func DetectInstallMethod(exePath string) InstallMethod {
	if m := readInstallMarker(filepath.Dir(exePath)); m != InstallUnknown {
		return m
	}
	return inferInstallMethod(exePath)
}

func readInstallMarker(dir string) InstallMethod {
	data, err := os.ReadFile(filepath.Join(dir, installMarkerName))
	if err != nil {
		return InstallUnknown
	}
	switch strings.ToLower(strings.TrimSpace(string(data))) {
	case "npm":
		return InstallNPM
	case "pnpm":
		return InstallPNPM
	case "yarn":
		return InstallYarn
	case "bun":
		return InstallBun
	case "brew", "homebrew":
		return InstallBrew
	case "brew-staging":
		return InstallBrewStaging
	case "script":
		return InstallScript
	default:
		return InstallUnknown
	}
}

func inferInstallMethod(exePath string) InstallMethod {
	lower := strings.ToLower(filepath.ToSlash(exePath))
	switch {
	case strings.Contains(lower, "/cellar/volcano-staging/"):
		return InstallBrewStaging
	case strings.Contains(lower, "/cellar/volcano/"):
		return InstallBrew
	case strings.Contains(lower, "node_modules/"+npmPackageName):
		switch {
		case strings.Contains(lower, "/.bun/"):
			return InstallBun
		case strings.Contains(lower, "/pnpm/"), strings.Contains(lower, "/.pnpm/"):
			return InstallPNPM
		case strings.Contains(lower, "/.yarn/"), strings.Contains(lower, "/yarn/"):
			return InstallYarn
		default:
			return InstallNPM
		}
	default:
		return InstallScript
	}
}

// UpgradeCommandFor maps an install method to the package-manager command that
// upgrades the CLI. managed is false for installs (script/manual/unknown) that
// `volcano upgrade` handles itself by replacing the binary in place.
func UpgradeCommandFor(m InstallMethod) (name string, args []string, managed bool) {
	switch m {
	case InstallNPM:
		return "npm", []string{"install", "-g", npmPackageName + "@latest"}, true
	case InstallPNPM:
		return "pnpm", []string{"add", "-g", npmPackageName + "@latest"}, true
	case InstallYarn:
		return "yarn", []string{"global", "add", npmPackageName + "@latest"}, true
	case InstallBun:
		return "bun", []string{"add", "-g", npmPackageName + "@latest"}, true
	case InstallBrew:
		return "brew", []string{"upgrade", "volcano"}, true
	case InstallBrewStaging:
		return "brew", []string{"upgrade", "volcano-staging"}, true
	default:
		return "", nil, false
	}
}
