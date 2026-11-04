package root

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// cliSurfacePath is Hosting's api/cli-surface.yaml, vendored next to the
// OpenAPI contract by Hosting's OpenAPI consumer sync. It records how this CLI
// reaches each Hosting operation.
var cliSurfacePath = filepath.Join("..", "..", "..", "openapi", "cli-surface.yaml")

type cliSurface struct {
	Reasons    map[string]string              `yaml:"reasons"`
	Operations map[string]cliSurfaceOperation `yaml:"operations"`
}

// cliSurfaceOperation is one route's entry, keyed by "METHOD /path". Hosting's
// test owns the rules for reasons and their links; this CLI checks only what it
// can answer: that the named command, flags and manifest section exist.
type cliSurfaceOperation struct {
	Command  string   `yaml:"command"`
	Flags    []string `yaml:"flags"`
	Manifest string   `yaml:"manifest"`
	Reason   string   `yaml:"reason"`
	Issue    string   `yaml:"issue"`
	PR       string   `yaml:"pr"`
}

// TestCLISurfaceCommandsExist fails when Hosting counts a route as covered by a
// command or flag this CLI does not have.
func TestCLISurfaceCommandsExist(t *testing.T) {
	surface := loadCLISurface(t)
	root := New(cliruntime.Deps{})

	for _, route := range sortedRoutes(surface) {
		operation := surface.Operations[route]
		if operation.Command == "" {
			continue
		}
		t.Run(route, func(t *testing.T) {
			cmd := findSurfaceCommand(t, root, operation.Command)
			if cmd == nil {
				return
			}
			for _, flag := range operation.Flags {
				name := strings.TrimPrefix(flag, "--")
				found := cmd.Flags().Lookup(name) != nil || cmd.InheritedFlags().Lookup(name) != nil
				assert.Truef(t, found, "%q has no flag %s", operation.Command, flag)
			}
		})
	}
}

// TestCLISurfaceManifestSectionsExist fails when Hosting counts a route as
// covered by a manifest section this CLI cannot decode.
func TestCLISurfaceManifestSectionsExist(t *testing.T) {
	surface := loadCLISurface(t)
	manifestType := reflect.TypeFor[projectconfig.Manifest]()

	for _, route := range sortedRoutes(surface) {
		section := surface.Operations[route].Manifest
		if section == "" {
			continue
		}
		assert.Truef(t, manifestHasSection(manifestType, section),
			"%s: the CLI manifest has no %q section", route, section)
	}
}

func TestCLISurfaceOperationsAreWellFormed(t *testing.T) {
	surface := loadCLISurface(t)
	for _, route := range sortedRoutes(surface) {
		operation := surface.Operations[route]
		set := 0
		for _, value := range []string{operation.Command, operation.Manifest, operation.Reason} {
			if value != "" {
				set++
			}
		}
		assert.Equalf(t, 1, set, "%s: needs exactly one of command, manifest or reason", route)
		if operation.Command == "" {
			assert.Emptyf(t, operation.Flags, "%s: flags need a command", route)
		}
		if operation.Reason != "" {
			assert.Containsf(t, surface.Reasons, operation.Reason, "%s: unknown reason", route)
		}
	}
}

func loadCLISurface(t *testing.T) cliSurface {
	t.Helper()
	data, err := os.ReadFile(cliSurfacePath)
	require.NoError(t, err, "Hosting's CLI surface must be vendored at openapi/cli-surface.yaml")

	var surface cliSurface
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	require.NoError(t, decoder.Decode(&surface))
	require.NotEmpty(t, surface.Operations)
	return surface
}

func sortedRoutes(surface cliSurface) []string {
	routes := make([]string, 0, len(surface.Operations))
	for route := range surface.Operations {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
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
	require.NoErrorf(t, err, "%q is not a command", path)
	if !assert.Emptyf(t, rest, "%q is not a command: %q has no subcommand %q", path, cmd.CommandPath(), strings.Join(rest, " ")) ||
		!assert.Equalf(t, path, cmd.CommandPath(), "name the command by its canonical path, not an alias") {
		return nil
	}
	// Local-mode stand-ins for cloud commands are hidden and take raw args.
	if !assert.Falsef(t, cmd.Hidden && cmd.DisableFlagParsing, "%q is a stand-in that only refuses; name the cloud command", path) {
		return nil
	}
	return cmd
}

// manifestHasSection follows a dotted path such as "auth.providers.oauth"
// through the manifest's yaml field names.
func manifestHasSection(typ reflect.Type, section string) bool {
	for name := range strings.SplitSeq(section, ".") {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return false
		}
		field, ok := yamlField(typ, name)
		if !ok {
			return false
		}
		typ = field.Type
	}
	return true
}

func yamlField(typ reflect.Type, name string) (reflect.StructField, bool) {
	for field := range typ.Fields() {
		tag, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if tag == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}
