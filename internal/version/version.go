// Package version exposes build-time identifiers injected via -ldflags.
package version

// Version, Commit, and Date are set at build time via -ldflags -X.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
