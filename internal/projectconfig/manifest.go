// Package projectconfig handles declarative cloud project configuration
// manifests (volcano-config.yaml) that describe storage buckets, storage
// policies, and function visibility for a project.
package projectconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

const (
	// ManifestVersion is the only currently supported manifest schema version.
	ManifestVersion = 1

	nestedManifestPath = "volcano/volcano-config.yaml"
	rootManifestPath   = "volcano-config.yaml"
)

// Manifest is the on-disk shape of volcano-config.yaml.
type Manifest struct {
	Version   int                `yaml:"version"`
	Buckets   []BucketManifest   `yaml:"buckets,omitempty"`
	Functions []FunctionManifest `yaml:"functions,omitempty"`
}

// BucketManifest declares one storage bucket and its desired policies.
type BucketManifest struct {
	Name             string           `yaml:"name"`
	FileSizeLimit    *int64           `yaml:"file_size_limit,omitempty"`
	AllowedMimeTypes *[]string        `yaml:"allowed_mime_types,omitempty"`
	Policies         []PolicyManifest `yaml:"policies,omitempty"`
}

// PolicyManifest declares one storage policy attached to a bucket.
type PolicyManifest struct {
	Name       string `yaml:"name"`
	Operation  string `yaml:"operation"`
	Definition string `yaml:"definition"`
}

// FunctionManifest declares the desired visibility for one deployed function.
type FunctionManifest struct {
	Name   string `yaml:"name"`
	Public *bool  `yaml:"public,omitempty"`
}

// Load reads, parses, and validates a manifest from disk. The returned path is
// the absolute path that was actually opened.
func Load(filePath string) (*Manifest, string, error) {
	if strings.TrimSpace(filePath) == "" {
		return nil, "", errors.New("file path is required")
	}

	resolved, err := filepath.Abs(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve file path: %w", err)
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read configuration file %q: %w", resolved, err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, "", fmt.Errorf("failed to parse YAML in %q: %w", resolved, err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, "", err
	}

	return &manifest, resolved, nil
}

// ResolveManifestPath returns the manifest path that `config deploy` should
// load. An explicit path is used as-is (after existence check); otherwise the
// CLI looks for volcano/volcano-config.yaml then volcano-config.yaml in the
// working directory.
func ResolveManifestPath(fileArg string) (string, error) {
	if trimmed := strings.TrimSpace(fileArg); trimmed != "" {
		if !fileExists(trimmed) {
			return "", fmt.Errorf("specified file not found: %s", trimmed)
		}
		return trimmed, nil
	}

	candidates := []string{nestedManifestPath, rootManifestPath}
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if fileExists(candidate) {
			found = append(found, candidate)
		}
	}

	switch len(found) {
	case 0:
		return "", errors.New("no volcano-config.yaml file found.\ncreate volcano/volcano-config.yaml or ./volcano-config.yaml, or use --file to specify a path")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("found multiple volcano-config.yaml files: %s.\nplease keep only one volcano-config.yaml file, or specify explicitly with --file", strings.Join(found, ", "))
	}
}

// Validate checks the manifest for structural and semantic errors, normalizing
// field values (trimming, uppercasing operations, dropping empty MIME entries)
// in place so callers receive a ready-to-deploy view.
func (m *Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d (expected %d)", m.Version, ManifestVersion)
	}

	if len(m.Buckets) == 0 && len(m.Functions) == 0 {
		return errors.New("manifest must include at least one bucket or function")
	}

	if err := validateBuckets(m.Buckets); err != nil {
		return err
	}
	return validateFunctions(m.Functions)
}

func validateBuckets(buckets []BucketManifest) error {
	seen := make(map[string]struct{}, len(buckets))
	for i := range buckets {
		bucket := &buckets[i]
		bucket.Name = strings.TrimSpace(bucket.Name)
		if bucket.Name == "" {
			return errors.New("bucket name is required")
		}
		if _, exists := seen[bucket.Name]; exists {
			return fmt.Errorf("duplicate bucket name %q in manifest", bucket.Name)
		}
		seen[bucket.Name] = struct{}{}

		if bucket.FileSizeLimit != nil && *bucket.FileSizeLimit <= 0 {
			return fmt.Errorf("bucket %q: file_size_limit must be greater than 0", bucket.Name)
		}

		if bucket.AllowedMimeTypes != nil {
			normalized := normalizeMIMEList(*bucket.AllowedMimeTypes)
			if len(*bucket.AllowedMimeTypes) > 0 && len(normalized) == 0 {
				return fmt.Errorf("bucket %q: allowed_mime_types contains only empty values", bucket.Name)
			}
			bucket.AllowedMimeTypes = &normalized
		}

		if err := validatePolicies(bucket); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicies(bucket *BucketManifest) error {
	seen := make(map[string]struct{}, len(bucket.Policies))
	for j := range bucket.Policies {
		policy := &bucket.Policies[j]
		policy.Name = strings.TrimSpace(policy.Name)
		if policy.Name == "" {
			return fmt.Errorf("bucket %q: policy name is required", bucket.Name)
		}
		if _, exists := seen[policy.Name]; exists {
			return fmt.Errorf("bucket %q: duplicate policy name %q", bucket.Name, policy.Name)
		}
		seen[policy.Name] = struct{}{}

		policy.Operation = strings.ToUpper(strings.TrimSpace(policy.Operation))
		if !apicommon.CreateStoragePolicyRequestOperation(policy.Operation).Valid() {
			return fmt.Errorf("bucket %q policy %q: operation must be SELECT, INSERT, UPDATE, or DELETE", bucket.Name, policy.Name)
		}

		policy.Definition = strings.TrimSpace(policy.Definition)
		if policy.Definition == "" {
			return fmt.Errorf("bucket %q policy %q: definition is required", bucket.Name, policy.Name)
		}
	}
	return nil
}

func validateFunctions(functions []FunctionManifest) error {
	seen := make(map[string]struct{}, len(functions))
	for i := range functions {
		function := &functions[i]
		function.Name = strings.TrimSpace(function.Name)
		if function.Name == "" {
			return errors.New("function name is required")
		}
		if _, exists := seen[function.Name]; exists {
			return fmt.Errorf("duplicate function name %q in manifest", function.Name)
		}
		seen[function.Name] = struct{}{}
		if function.Public == nil {
			return fmt.Errorf("function %q: public is required", function.Name)
		}
	}
	return nil
}

func normalizeMIMEList(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}
