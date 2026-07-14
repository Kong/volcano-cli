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
