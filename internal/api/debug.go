package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// debugOut is where traces are written. Overridable in tests; stderr in prod so
// it never pollutes stdout/JSON output.
var debugOut io.Writer = os.Stderr

// debugEnabled turns on stderr tracing of API requests/responses. It's seeded
// from VOLCANO_DEBUG at startup and can be flipped by the root --debug flag.
var debugEnabled atomic.Bool

func init() {
	if envTruthy(os.Getenv("VOLCANO_DEBUG")) {
		debugEnabled.Store(true)
	}
}

// SetDebug enables or disables API request/response tracing to stderr.
func SetDebug(on bool) { debugEnabled.Store(on) }

// DebugEnabled reports whether API tracing is on.
func DebugEnabled() bool { return debugEnabled.Load() }

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// debugDoer traces each request and response to stderr when debug is enabled.
// It sits closest to the wire so it observes the final headers (auth + version
// protocol). It never prints secrets: the Authorization header is reduced to
// its scheme, other credential-ish headers are shown as <redacted>, and request
// and response bodies are not logged.
type debugDoer struct {
	next apiclient.HttpRequestDoer
}

func (d debugDoer) Do(req *http.Request) (*http.Response, error) {
	if !debugEnabled.Load() {
		return d.next.Do(req)
	}
	fmt.Fprintf(debugOut, "volcano: \u2192 %s %s\n", req.Method, req.URL.Redacted())
	for _, name := range sortedHeaderNames(req.Header) {
		fmt.Fprintf(debugOut, "volcano:   %s: %s\n", name, redactHeaderValue(name, req.Header.Values(name)))
	}
	start := time.Now()
	resp, err := d.next.Do(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Fprintf(debugOut, "volcano: \u2190 error after %s: %v\n", elapsed, err)
		return resp, err
	}
	if resp != nil {
		fmt.Fprintf(debugOut, "volcano: \u2190 %s (%s)\n", resp.Status, elapsed)
	}
	return resp, err
}

func sortedHeaderNames(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// redactHeaderValue keeps a trace useful for debugging auth ("is the header
// present, and with what scheme?") without ever printing a token value.
func redactHeaderValue(name string, values []string) string {
	if !sensitiveHeader(name) {
		return strings.Join(values, ", ")
	}
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return "(absent)"
	}
	// For Authorization, reveal the scheme only when it is a recognized scheme
	// cleanly separated from the credential; otherwise fully redact, so a
	// scheme-less or oddly-formatted value can never leak the credential itself.
	if strings.EqualFold(name, "Authorization") {
		if scheme, ok := knownAuthScheme(values[0]); ok {
			return scheme + " <redacted>"
		}
		return "<redacted>"
	}
	return "<redacted>"
}

// knownAuthScheme returns the leading auth scheme when v is "<scheme> <credential>"
// for a recognized scheme, so only the scheme (never the credential) is shown.
func knownAuthScheme(v string) (string, bool) {
	i := strings.IndexByte(v, ' ')
	if i <= 0 || i >= len(v)-1 {
		return "", false
	}
	scheme := v[:i]
	switch strings.ToLower(scheme) {
	case "bearer", "basic", "digest", "token":
		return scheme, true
	}
	return "", false
}

func sensitiveHeader(name string) bool {
	n := strings.ToLower(name)
	for _, needle := range []string{"authorization", "token", "cookie", "secret", "api-key", "apikey"} {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return false
}
