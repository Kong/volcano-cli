package project

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/envfile"
)

// CloudEnvRelPath is the env file cloud deploys read, as `volcano init` names
// it. `volcano connect` writes the project's API URL and anon key to it.
const CloudEnvRelPath = "volcano/cloud.env"

// anonKeyKeys are the cloud env keys that hold a project's anon key, the
// only value `volcano connect` writes that differs between projects.
var anonKeyKeys = []string{"VOLCANO_ANON_KEY", "NEXT_PUBLIC_VOLCANO_ANON_KEY"}

// WriteCloudEnv sets the project's API URL and anon key in dir's cloud env
// file, keeping every other line. It creates the file when it is missing. The
// URL is written without userinfo, query or fragment: a Next.js build puts
// NEXT_PUBLIC_VOLCANO_API_URL in browser code.
func WriteCloudEnv(dir, apiURL, anonKey string) error {
	apiURL = api.PublicURL(apiURL)
	return envfile.Upsert(CloudEnvPath(dir), []envfile.Var{
		{Key: "VOLCANO_API_URL", Value: apiURL},
		{Key: "VOLCANO_ANON_KEY", Value: anonKey},
		{Key: "NEXT_PUBLIC_VOLCANO_API_URL", Value: apiURL},
		{Key: "NEXT_PUBLIC_VOLCANO_ANON_KEY", Value: anonKey},
	}, "# Cloud project values, written by `volcano connect`. Deploy with:",
		"#   volcano cloud deploy --env-file volcano/cloud.env")
}

// refreshCloudEnv rewrites the keys `volcano connect` wrote in dir's cloud env
// file for projectID, so a deploy with that file does not upload another
// project's anon key. A file without an anon key, or no file, is left alone.
// It returns the file's path when it changed it.
func refreshCloudEnv(ctx context.Context, client *api.Client, dir, apiURL string, projectID uuid.UUID) (string, error) {
	path := CloudEnvPath(dir)
	managed, err := envfile.HasKey(path, anonKeyKeys...)
	if err != nil || !managed {
		return "", err
	}
	anonKey, err := DefaultAnonKey(ctx, client, projectID)
	if err != nil {
		return "", err
	}
	if err := WriteCloudEnv(dir, apiURL, anonKey); err != nil {
		return "", err
	}
	return path, nil
}

// CloudEnvPath is the cloud env file in dir.
func CloudEnvPath(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(CloudEnvRelPath))
}

// DefaultAnonKey returns the project's default anon key, the publishable key
// the SDK and frontends use. It creates one only when the project has none.
// The API lists the newest keys first, so the default, usually the oldest, can
// be on the last page.
func DefaultAnonKey(ctx context.Context, client *api.Client, projectID uuid.UUID) (string, error) {
	var fallback string
	for page := api.DefaultPage; ; page++ {
		keys, more, err := client.ListAnonKeysPage(ctx, projectID, page, api.DefaultLimit)
		if err != nil {
			return "", fmt.Errorf("failed to get the project's anon key: %w", err)
		}
		for _, key := range keys {
			if key.KeyValue == "" {
				continue
			}
			if key.IsDefault != nil && *key.IsDefault {
				return key.KeyValue, nil
			}
			if fallback == "" {
				fallback = key.KeyValue
			}
		}
		if !more || len(keys) == 0 {
			break
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	key, err := client.CreateAnonKey(ctx, projectID, "default")
	if err != nil {
		return "", fmt.Errorf("failed to create an anon key: %w", err)
	}
	return key.KeyValue, nil
}
