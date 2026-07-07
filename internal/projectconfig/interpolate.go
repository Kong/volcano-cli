package projectconfig

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// interpolateNode expands ${ENV} references in every string scalar value of
// the YAML document. Mapping keys are left untouched.
func interpolateNode(node *yaml.Node, lookupEnv func(string) (string, bool)) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := interpolateNode(child, lookupEnv); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			if err := interpolateNode(node.Content[i], lookupEnv); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return nil
		}
		expanded, err := interpolateString(node.Value, lookupEnv)
		if err != nil {
			return err
		}
		node.Value = expanded
	}
	return nil
}

// interpolateString expands ${NAME} references from lookupEnv. A reference to
// an unset variable is a hard error; `$$` produces a literal `$`; any other
// `$` passes through unchanged.
func interpolateString(input string, lookupEnv func(string) (string, bool)) (string, error) {
	if !strings.ContainsRune(input, '$') {
		return input, nil
	}

	var out strings.Builder
	out.Grow(len(input))
	for i := 0; i < len(input); i++ {
		if input[i] != '$' {
			out.WriteByte(input[i])
			continue
		}
		if i+1 < len(input) && input[i+1] == '$' {
			out.WriteByte('$')
			i++
			continue
		}
		if i+1 < len(input) && input[i+1] == '{' {
			end := strings.IndexByte(input[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated ${...} reference in %q", input)
			}
			name := input[i+2 : i+2+end]
			if strings.TrimSpace(name) == "" {
				return "", fmt.Errorf("empty ${} reference in %q", input)
			}
			value, ok := lookupEnv(name)
			if !ok {
				return "", fmt.Errorf("environment variable %q is not set (referenced as ${%s}); export it or use $$ for a literal $", name, name)
			}
			out.WriteString(value)
			// Skip past "{NAME}"; the loop increment consumes the "$".
			i += end + 2
			continue
		}
		out.WriteByte('$')
	}
	return out.String(), nil
}
