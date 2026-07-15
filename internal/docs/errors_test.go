package docs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCodePrefersSyncIncompleteOverSourceUnavailable(t *testing.T) {
	// A partial download wraps both (outer ErrSyncIncomplete, cause ErrSourceUnavailable).
	err := fmt.Errorf("%w: %w", ErrSyncIncomplete, fmt.Errorf("%w: GET x -> 500", ErrSourceUnavailable))
	assert.Equal(t, "DOCS_SYNC_INCOMPLETE", Code(err))
	// A pure source failure still maps to DOCS_SOURCE_UNAVAILABLE.
	assert.Equal(t, "DOCS_SOURCE_UNAVAILABLE", Code(fmt.Errorf("%w: boom", ErrSourceUnavailable)))
}

func TestCodeOfflineConflictIsDistinctFromSourceUnavailable(t *testing.T) {
	// `sync --offline` is an invalid flag combination, not a source failure.
	assert.Equal(t, "DOCS_OFFLINE_CONFLICT", Code(fmt.Errorf("%w: cannot sync with --offline", ErrOfflineConflict)))
}

func TestIsRealGitHubAPIRequiresHTTPS(t *testing.T) {
	assert.True(t, isRealGitHubAPI("https://api.github.com"))
	// Plaintext to the API host must not be treated as trusted (no token attached).
	assert.False(t, isRealGitHubAPI("http://api.github.com"))
	assert.False(t, isRealGitHubAPI("https://evil.example.com"))
}
