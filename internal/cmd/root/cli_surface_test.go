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
	Gaps       map[string]string              `yaml:"gaps"`
	Operations map[string]cliSurfaceOperation `yaml:"operations"`
}

type cliSurfaceOperation struct {
	Command  string `yaml:"command"`
	Manifest string `yaml:"manifest"`
	Reason   string `yaml:"reason"`
	Gap      string `yaml:"gap"`
}

// TestCLISurfaceCommandsExist fails when Hosting counts an operation as covered
// by a command this CLI does not have.
func TestCLISurfaceCommandsExist(t *testing.T) {
	surface := loadCLISurface(t)
	root := New(cliruntime.Deps{})

	for _, operationID := range sortedOperationIDs(surface) {
		command := surface.Operations[operationID].Command
		if command == "" {
			continue
		}
		t.Run(operationID, func(t *testing.T) {
			assertSurfaceCommand(t, root, command)
		})
	}
}

// TestCLISurfaceManifestSectionsExist fails when Hosting counts an operation as
// covered by a manifest section this CLI cannot decode.
func TestCLISurfaceManifestSectionsExist(t *testing.T) {
	surface := loadCLISurface(t)
	manifestType := reflect.TypeFor[projectconfig.Manifest]()

	for _, operationID := range sortedOperationIDs(surface) {
		section := surface.Operations[operationID].Manifest
		if section == "" {
			continue
		}
		assert.Truef(t, manifestHasSection(manifestType, section),
			"%s: the CLI manifest has no %q section", operationID, section)
	}
}

func TestCLISurfaceOperationsAreWellFormed(t *testing.T) {
	surface := loadCLISurface(t)
	for _, operationID := range sortedOperationIDs(surface) {
		operation := surface.Operations[operationID]
		set := 0
		for _, value := range []string{operation.Command, operation.Manifest, operation.Reason, operation.Gap} {
			if value != "" {
				set++
			}
		}
		assert.Equalf(t, 1, set, "%s: needs exactly one of command, manifest, reason or gap", operationID)
		if operation.Reason != "" {
			assert.Containsf(t, surface.Reasons, operation.Reason, "%s: unknown reason", operationID)
		}
		if operation.Gap != "" {
			assert.Containsf(t, surface.Gaps, operation.Gap, "%s: unknown gap", operationID)
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

func sortedOperationIDs(surface cliSurface) []string {
	ids := make([]string, 0, len(surface.Operations))
	for id := range surface.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// assertSurfaceCommand resolves a full command path such as
// "volcano cloud databases branches create" in the cobra tree.
func assertSurfaceCommand(t *testing.T, root *cobra.Command, path string) {
	t.Helper()
	words := strings.Fields(path)
	if !assert.Greaterf(t, len(words), 1, "%q is not a subcommand", path) ||
		!assert.Equalf(t, root.Name(), words[0], "%q must start with %q", path, root.Name()) {
		return
	}
	cmd, rest, err := root.Find(words[1:])
	require.NoErrorf(t, err, "%q is not a command", path)
	if !assert.Emptyf(t, rest, "%q is not a command: %q has no subcommand %q", path, cmd.CommandPath(), strings.Join(rest, " ")) ||
		!assert.Equalf(t, path, cmd.CommandPath(), "name the command by its canonical path, not an alias") {
		return
	}
	// Local-mode stand-ins for cloud commands are hidden and take raw args.
	assert.Falsef(t, cmd.Hidden && cmd.DisableFlagParsing, "%q is a stand-in that only refuses; name the cloud command", path)
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
