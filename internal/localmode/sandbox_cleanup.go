package localmode

import (
	"context"
	"fmt"
	"strings"
)

// The broker owns resources outside Compose. Stop producers first, then reclaim
// only its namespace before deleting the volumes needed to retry cleanup.
func (s Service) cleanSandboxResources(ctx context.Context) error {
	filter := "label=dev.volcano.sandbox.namespace=" + composeProjectName
	containers, err := s.runDocker(ctx, "container", "ls", "--all", "--quiet", "--filter", filter)
	if err != nil {
		return fmt.Errorf("list local Sandbox containers: %w", err)
	}
	for id := range strings.FieldsSeq(string(containers)) {
		if _, err := s.runDocker(ctx, "container", "rm", "--force", id); err != nil {
			return fmt.Errorf("remove local Sandbox container %s: %w", id, err)
		}
	}
	networks, err := s.runDocker(ctx, "network", "ls", "--quiet", "--filter", filter)
	if err != nil {
		return fmt.Errorf("list local Sandbox networks: %w", err)
	}
	for id := range strings.FieldsSeq(string(networks)) {
		// Docker refuses to remove a network with remaining endpoints. Never
		// disconnect unrelated containers to force cleanup through.
		if _, err := s.runDocker(ctx, "network", "rm", id); err != nil {
			return fmt.Errorf("remove local Sandbox network %s: %w", id, err)
		}
	}
	images, err := s.runDocker(ctx, "image", "ls", "--quiet", "--filter", filter)
	if err != nil {
		return fmt.Errorf("list local Sandbox images: %w", err)
	}
	seen := make(map[string]bool)
	for id := range strings.FieldsSeq(string(images)) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.runDocker(ctx, "image", "rm", id); err != nil {
			return fmt.Errorf("remove local Sandbox image %s: %w", id, err)
		}
	}
	return nil
}
