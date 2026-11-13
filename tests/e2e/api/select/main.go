// Package main selects CLI API E2E tests supported by a Hosting OpenAPI spec.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const suitePattern = `^TestAPIE2E(?:Smoke|Cloud)`

type declarations struct {
	Common []string            `yaml:"common"`
	Tests  map[string][]string `yaml:"tests"`
}

func main() {
	cliSpec := flag.String("cli-openapi", "openapi/openapi.yaml", "CLI OpenAPI spec")
	hostingSpec := flag.String("hosting-openapi", "openapi/openapi.yaml", "Hosting OpenAPI spec")
	manifest := flag.String("declarations", "tests/e2e/api/capabilities.yaml", "test capability declarations")
	testDir := flag.String("tests", "tests/e2e/api", "API E2E test directory")
	match := flag.String("match", `^TestAPIE2E(?:Smoke|Cloud)`, "tests eligible for this run")
	flag.Parse()

	selected, err := selectTests(*cliSpec, *hostingSpec, *manifest, *testDir, *match)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(testRegexp(selected))
}

func selectTests(cliSpecPath, hostingSpecPath, manifestPath, testDir, match string) ([]string, error) {
	cliOperations, err := operationIDs(cliSpecPath)
	if err != nil {
		return nil, fmt.Errorf("read CLI OpenAPI operations: %w", err)
	}
	hostingOperations, err := operationIDs(hostingSpecPath)
	if err != nil {
		return nil, fmt.Errorf("read Hosting OpenAPI operations: %w", err)
	}
	declared, err := readDeclarations(manifestPath)
	if err != nil {
		return nil, err
	}
	tests, err := testNames(testDir)
	if err != nil {
		return nil, err
	}
	if err := validateDeclarations(declared, tests, cliOperations); err != nil {
		return nil, err
	}
	runPattern, err := regexp.Compile(match)
	if err != nil {
		return nil, fmt.Errorf("compile test match %q: %w", match, err)
	}

	selected := make([]string, 0, len(tests))
	for _, test := range tests {
		if !runPattern.MatchString(test) {
			continue
		}
		required := append(slices.Clone(declared.Common), declared.Tests[test]...)
		if allPresent(required, hostingOperations) {
			selected = append(selected, test)
		}
	}
	return selected, nil
}

func operationIDs(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 {
		return nil, errors.New("OpenAPI spec is empty")
	}
	root := document.Content[0]
	paths := mappingValue(root, "paths")
	if paths == nil {
		return nil, errors.New("OpenAPI spec has no paths")
	}
	operations := make(map[string]bool)
	for i := 1; i < len(paths.Content); i += 2 {
		pathItem := paths.Content[i]
		for j := 1; j < len(pathItem.Content); j += 2 {
			operation := pathItem.Content[j]
			if operation.Kind != yaml.MappingNode {
				continue
			}
			if operationID := mappingValue(operation, "operationId"); operationID != nil {
				operations[operationID.Value] = true
			}
		}
	}
	return operations, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func readDeclarations(path string) (declarations, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return declarations{}, fmt.Errorf("read capability declarations: %w", err)
	}
	var result declarations
	if err := yaml.Unmarshal(data, &result); err != nil {
		return declarations{}, fmt.Errorf("parse capability declarations: %w", err)
	}
	return result, nil
}

func testNames(dir string) ([]string, error) {
	pattern := regexp.MustCompile(suitePattern)
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, fmt.Errorf("find API E2E tests: %w", err)
	}
	var names []string
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && pattern.MatchString(function.Name.Name) {
				names = append(names, function.Name.Name)
			}
		}
	}
	slices.Sort(names)
	return names, nil
}

func validateDeclarations(declared declarations, tests []string, knownOperations map[string]bool) error {
	knownTests := make(map[string]bool, len(tests))
	for _, test := range tests {
		knownTests[test] = true
		if len(declared.Tests[test]) == 0 {
			return fmt.Errorf("API E2E test %s has no capability declaration", test)
		}
	}
	for test, operations := range declared.Tests {
		if !knownTests[test] {
			return fmt.Errorf("capability declaration names unknown API E2E test %s", test)
		}
		if err := validateOperations(test, operations, knownOperations); err != nil {
			return err
		}
	}
	return validateOperations("common", declared.Common, knownOperations)
}

func validateOperations(owner string, operations []string, known map[string]bool) error {
	if len(operations) == 0 {
		return fmt.Errorf("%s has no capability declaration", owner)
	}
	seen := make(map[string]bool, len(operations))
	for _, operation := range operations {
		if !known[operation] {
			return fmt.Errorf("%s requires unknown OpenAPI operationId %s", owner, operation)
		}
		if seen[operation] {
			return fmt.Errorf("%s repeats OpenAPI operationId %s", owner, operation)
		}
		seen[operation] = true
	}
	return nil
}

func allPresent(required []string, available map[string]bool) bool {
	for _, operation := range required {
		if !available[operation] {
			return false
		}
	}
	return true
}

func testRegexp(tests []string) string {
	if len(tests) == 0 {
		return `^$`
	}
	quoted := make([]string, len(tests))
	for i, test := range tests {
		quoted[i] = regexp.QuoteMeta(test)
	}
	return `^(?:` + strings.Join(quoted, `|`) + `)$`
}
