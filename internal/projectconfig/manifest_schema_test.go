package projectconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestManifestAcceptsEveryProjectConfigField walks the server's ProjectConfig
// schema in the vendored OpenAPI contract and fails for any field the strict
// decoder would refuse. `config pull` writes what the server exports, so a
// field missing here makes a pulled manifest fail `config deploy`.
func TestManifestAcceptsEveryProjectConfigField(t *testing.T) {
	schemas := loadOpenAPISchemas(t)
	var missing []string
	walkSchemaFields(schemas, schemas["ProjectConfig"], reflect.TypeFor[Manifest](), "", map[string]bool{}, &missing)
	sort.Strings(missing)

	// Each pending field is being added by an open pull request. It fails here
	// once the field decodes, so the entry is removed when that change lands.
	pending := map[string]string{}
	for field, change := range pending {
		assert.Containsf(t, missing, field, "%s now decodes; remove its pending entry (%s)", field, change)
		missing = slices.DeleteFunc(missing, func(m string) bool { return m == field })
	}
	// Each refused field is one the API ignores and never exports, which the
	// manifest refuses with its own message rather than decode.
	refused := map[string]string{"lavaflow": "LavaFlow is turned on with its own commands"}
	for field, why := range refused {
		assert.Containsf(t, missing, field, "%s now decodes, but the manifest refuses it: %s", field, why)
		missing = slices.DeleteFunc(missing, func(m string) bool { return m == field })
	}
	assert.Empty(t, missing, "ProjectConfig fields the CLI manifest cannot decode")
}

func loadOpenAPISchemas(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "openapi", "openapi.yaml"))
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(data, &spec))
	require.Contains(t, spec.Components.Schemas, "ProjectConfig")
	return spec.Components.Schemas
}

// walkSchemaFields records each schema property path that has no matching yaml
// field on typ. Keyed maps (additionalProperties) are not walked into: their
// keys are data, not field names.
func walkSchemaFields(schemas map[string]any, node any, typ reflect.Type, path string, seen map[string]bool, missing *[]string) {
	schema, refs := resolveSchema(schemas, node)
	for _, ref := range refs {
		if seen[ref] {
			return
		}
	}
	for _, ref := range refs {
		seen[ref] = true
	}
	defer func() {
		for _, ref := range refs {
			delete(seen, ref)
		}
	}()
	if schema == nil {
		return
	}
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if items, ok := schema["items"]; ok {
		walkSchemaFields(schemas, items, typ, path+"[]", seen, missing)
		return
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		variants, _ := schema[key].([]any)
		for _, variant := range variants {
			walkSchemaFields(schemas, variant, typ, path, seen, missing)
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for name, property := range properties {
		fieldPath := strings.TrimPrefix(path+"."+name, ".")
		field, ok := yamlFieldByName(typ, name)
		if !ok {
			*missing = append(*missing, fieldPath)
			continue
		}
		walkSchemaFields(schemas, property, field.Type, fieldPath, seen, missing)
	}
}

// resolveSchema follows local $refs and returns the schema with the names it
// passed through, so the caller can stop a recursive schema from looping.
func resolveSchema(schemas map[string]any, node any) (map[string]any, []string) {
	var refs []string
	schema, _ := node.(map[string]any)
	for schema != nil {
		ref, ok := schema["$ref"].(string)
		if !ok {
			break
		}
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		refs = append(refs, name)
		schema, _ = schemas[name].(map[string]any)
	}
	return schema, refs
}

func yamlFieldByName(typ reflect.Type, name string) (reflect.StructField, bool) {
	for field := range typ.Fields() {
		tag, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if tag == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}
