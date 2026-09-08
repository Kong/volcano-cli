package projectconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// ErrUnsafePulledManifest reports that a downloaded manifest could not be
// checked or cleaned of variable values, so nothing was written. The download
// itself succeeded; callers match it with errors.Is to avoid framing this as a
// transport failure.
var ErrUnsafePulledManifest = errors.New("downloaded configuration could not be checked for variable values")

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
// A manifest that cannot be parsed is rejected rather than written: bytes we
// could not inspect cannot be promised to be value-free.
func sanitizePulledManifest(manifest []byte) ([]byte, bool, error) {
	if len(bytes.TrimSpace(manifest)) == 0 {
		return manifest, false, nil
	}

	decoder := yaml.NewDecoder(bytes.NewReader(manifest))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return nil, false, fmt.Errorf("%w: the server returned a manifest that is not valid YAML: %w", ErrUnsafePulledManifest, err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, false, fmt.Errorf("%w: the server returned a manifest that is not valid YAML: %w", ErrUnsafePulledManifest, err)
		}
		return nil, false, fmt.Errorf("%w: the server returned multiple YAML documents", ErrUnsafePulledManifest)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		// Nothing that could hold a `variables` mapping key; leave it verbatim
		// and let the normal manifest parsing report the shape problem.
		return manifest, false, nil
	}

	root := doc.Content[0]
	kept := make([]*yaml.Node, 0, len(root.Content))
	stripped := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "variables" {
			stripped = true
			continue
		}
		kept = append(kept, root.Content[i], root.Content[i+1])
	}
	if !stripped {
		return manifest, false, nil
	}
	root.Content = kept

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(pulledManifestIndent)
	if err := encoder.Encode(&doc); err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrUnsafePulledManifest, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrUnsafePulledManifest, err)
	}
	return buf.Bytes(), true, nil
}
