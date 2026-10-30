package root

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// cliSurfacePath is Hosting's route-to-command list, vendored next to the
// OpenAPI contract by Hosting's OpenAPI consumer sync.
var cliSurfacePath = filepath.Join("..", "..", "..", "openapi", "cli-surface.yaml")

type cliSurfaceEntry struct {
	Command  string   `yaml:"command"`
	Flags    []string `yaml:"flags"`
	Manifest string   `yaml:"manifest"`
	Reason   string   `yaml:"reason"`
	Issue    string   `yaml:"issue"`
}

// TestCLISurfaceCommandsExist fails when Hosting names a command for a route
// that this CLI does not have, so a route Hosting counts as covered really is.
func TestCLISurfaceCommandsExist(t *testing.T) {
	entries := loadCLISurface(t)
	root := New(cliruntime.Deps{})

	routes := make([]string, 0, len(entries))
	for route := range entries {
		routes = append(routes, route)
	}
	sort.Strings(routes)

	for _, route := range routes {
		entry := entries[route]
		if entry.Command == "" {
			continue
		}
		t.Run(route, func(t *testing.T) {
			cmd := findSurfaceCommand(t, root, entry.Command)
			if cmd == nil {
				return
			}
			for _, flag := range entry.Flags {
				name := strings.TrimPrefix(flag, "--")
				found := cmd.Flags().Lookup(name) != nil || cmd.InheritedFlags().Lookup(name) != nil
				assert.Truef(t, found, "%s: %q has no flag %s", route, entry.Command, flag)
			}
		})
	}
}

func TestCLISurfaceEntriesAreWellFormed(t *testing.T) {
	for route, entry := range loadCLISurface(t) {
		hasCommand := entry.Command != ""
		hasReason := entry.Reason != ""
		assert.Truef(t, hasCommand != hasReason, "%s: needs exactly one of command or reason", route)
		if entry.Reason == "gap" {
			assert.NotEmptyf(t, entry.Issue, "%s: a gap needs its issue", route)
		}
		if !hasCommand {
			assert.Emptyf(t, entry.Flags, "%s: flags need a command", route)
			assert.Emptyf(t, entry.Manifest, "%s: manifest needs a command", route)
		}
	}
}

func loadCLISurface(t *testing.T) map[string]cliSurfaceEntry {
	t.Helper()
	data, err := os.ReadFile(cliSurfacePath)
	require.NoError(t, err, "Hosting's route-to-command list must be vendored at openapi/cli-surface.yaml")

	var entries map[string]cliSurfaceEntry
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&entries))
	require.NotEmpty(t, entries)
	return entries
}

// findSurfaceCommand resolves a full command path such as
// "volcano cloud databases branches create" in the cobra tree.
func findSurfaceCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	words := strings.Fields(path)
	if !assert.Greaterf(t, len(words), 1, "%q is not a subcommand", path) ||
		!assert.Equalf(t, root.Name(), words[0], "%q must start with %q", path, root.Name()) {
		return nil
	}
	cmd, rest, err := root.Find(words[1:])
	if !assert.NoErrorf(t, err, "%q is not a command", path) ||
		!assert.Emptyf(t, rest, "%q is not a command: %q has no subcommand %q", path, cmd.CommandPath(), strings.Join(rest, " ")) {
		return nil
	}
	if !assert.Equalf(t, path, cmd.CommandPath(), "name the command by its canonical path, not an alias") {
		return nil
	}
	// Local-mode stand-ins for cloud commands are hidden and take raw args.
	if !assert.Falsef(t, cmd.Hidden && cmd.DisableFlagParsing, "%q is a stand-in that only refuses; name the cloud command", path) {
		return nil
	}
	return cmd
}
