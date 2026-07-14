package docs

import "errors"

// Sentinel errors carry stable machine codes for the JSON envelope. Each maps
// to a DOCS_* code in the command layer.
var (
	// ErrCacheMissing indicates no cache snapshot exists yet (and, for reads,
	// --offline prevented a bootstrap sync).
	ErrCacheMissing = errors.New("docs cache is empty; run `volcano docs sync`")
	// ErrSourceUnavailable indicates the GitHub source could not be reached or
	// returned an error while syncing.
	ErrSourceUnavailable = errors.New("docs source unavailable")
	// ErrInvalidSource indicates a malformed repo/ref/path.
	ErrInvalidSource = errors.New("invalid docs source")
	// ErrDocNotFound indicates a requested document/section does not exist.
	ErrDocNotFound = errors.New("doc not found")
	// ErrInvalidID indicates a malformed document id/path.
	ErrInvalidID = errors.New("invalid doc id")
	// ErrSyncIncomplete indicates a sync failed partway and left the previous
	// cache untouched.
	ErrSyncIncomplete = errors.New("docs sync incomplete")
)

// Code returns the DOCS_* machine code for a docs error, or empty string.
func Code(err error) string {
	switch {
	case errors.Is(err, ErrCacheMissing):
		return "DOCS_CACHE_MISSING"
	case errors.Is(err, ErrInvalidSource):
		return "DOCS_INVALID_SOURCE"
	case errors.Is(err, ErrInvalidID):
		return "DOCS_INVALID_ID"
	case errors.Is(err, ErrDocNotFound):
		return "DOCS_NOT_FOUND"
	// A partial download wraps both ErrSyncIncomplete (outer) and
	// ErrSourceUnavailable (cause); prefer the more specific outer semantic.
	case errors.Is(err, ErrSyncIncomplete):
		return "DOCS_SYNC_INCOMPLETE"
	case errors.Is(err, ErrSourceUnavailable):
		return "DOCS_SOURCE_UNAVAILABLE"
	default:
		return ""
	}
}
