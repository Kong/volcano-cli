package projectconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"

	"gopkg.in/yaml.v3"
)

// ErrUnsafePulledManifest reports that a downloaded manifest could not be
// checked or cleaned of variable values, so nothing was written. The download
// itself succeeded; callers match it with errors.Is to avoid framing this as a
// transport failure.
var ErrUnsafePulledManifest = errors.New("downloaded configuration could not be checked for variable values")

// Sections sanitizePulledManifest removes, as reported in PullResult.Removed.
const (
	RemovedVariables            = "variables"
	RemovedNotificationsWebhook = "notifications.webhook"
)

// pulledManifestIndent matches the two-space indentation the server uses in the
// canonical YAML rendering, so a re-marshaled manifest still reads like a pulled
// one.
const pulledManifestIndent = 2

// sanitizePulledManifest enforces the export contract client-side: a pulled
// manifest carries variable names and shared membership, never variable values.
// The server is expected to omit the top-level `variables` section from exports
// already, so the common path is a no-op that returns the response bytes
// untouched, preserving the canonical rendering byte for byte (comments,
// ordering, and all). Only when a server does return `variables` — an older or
// misbehaving one — is the section removed and the manifest re-marshaled.
//
// The whole section is dropped rather than just each `value`, because
// ProjectConfigVariable requires both `name` and `value`: a value-less
// `variables` list would fail server validation on the next deploy, while
// omitting the section keeps the pulled manifest deployable and leaves the
// project's variable values untouched (omitted sections are not reconciled).
//
// `notifications.webhook` goes the same way: its URL can carry the receiver's
// token, and the manifest is a file users commit.
//
// A manifest that cannot be parsed is rejected rather than written: bytes we
// could not inspect cannot be promised to be value-free.
func sanitizePulledManifest(manifest []byte) ([]byte, []string, error) {
	if len(bytes.TrimSpace(manifest)) == 0 {
		return manifest, nil, nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(manifest))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("%w: the server returned a manifest that is not valid YAML: %w", ErrUnsafePulledManifest, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, nil, fmt.Errorf("%w: the server returned a manifest that is not valid YAML: %w", ErrUnsafePulledManifest, err)
		}
		return nil, nil, fmt.Errorf("%w: the server returned multiple YAML documents", ErrUnsafePulledManifest)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%w: the server returned a manifest with an unsupported YAML root", ErrUnsafePulledManifest)
	}

	root := doc.Content[0]
	if containsYAMLAlias(root) {
		return nil, nil, fmt.Errorf("%w: the server returned unsupported YAML aliases", ErrUnsafePulledManifest)
	}
	kept := make([]*yaml.Node, 0, len(root.Content))
	var removed []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, nil, fmt.Errorf("%w: the server returned unsupported YAML mapping keys", ErrUnsafePulledManifest)
		}
		value := root.Content[i+1]
		if key.Value == "variables" {
			removed = append(removed, RemovedVariables)
			continue
		}
		if key.Value == "notifications" && dropYAMLKey(value, "webhook") {
			removed = append(removed, RemovedNotificationsWebhook)
			if len(value.Content) == 0 {
				continue
			}
		}
		kept = append(kept, key, value)
	}
	if len(removed) == 0 {
		return manifest, nil, nil
	}
	root.Content = kept

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(pulledManifestIndent)
	if err := encoder.Encode(&doc); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnsafePulledManifest, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrUnsafePulledManifest, err)
	}
	return buf.Bytes(), removed, nil
}

// dropYAMLKey removes key from a mapping node and reports whether it was there.
// A yaml.Node keeps duplicate keys, so every occurrence goes.
func dropYAMLKey(mapping *yaml.Node, key string) bool {
	if mapping.Kind != yaml.MappingNode {
		return false
	}
	removed := false
	for i := 0; i+1 < len(mapping.Content); {
		if mapping.Content[i].Value == key {
			mapping.Content = slices.Delete(mapping.Content, i, i+2)
			removed = true
			continue
		}
		i += 2
	}
	return removed
}

func containsYAMLAlias(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	return slices.ContainsFunc(node.Content, containsYAMLAlias)
}
