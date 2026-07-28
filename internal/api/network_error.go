package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// envAPIURL is the environment variable that overrides the API URL. Duplicated
// as a literal here (the canonical definition is config.envAPIURL, unexported)
// only to attribute the URL's source in the unreachable-API message below.
const envAPIURL = "VOLCANO_API_URL"

// networkErrorDoer turns opaque transport failures (connection refused, DNS
// failure, timeout) into an actionable message that names the API URL being
// targeted — so a down server or a mis-set VOLCANO_API_URL surfaces as guidance
// instead of a raw Go dial error like
// `dial tcp 127.0.0.1:8000: connect: connection refused`. It rewrites only
// genuine "never reached the server" errors; successful requests, HTTP error
// responses, and all other errors pass through untouched.
type networkErrorDoer struct {
	apiURL string
	next   apiclient.HttpRequestDoer
}

func (d networkErrorDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if err == nil {
		return resp, nil
	}
	hint := unreachableHint(err)
	if hint == "" {
		return resp, err
	}
	source := ""
	if env := strings.TrimRight(strings.TrimSpace(os.Getenv(envAPIURL)), "/"); env != "" && env == d.apiURL {
		source = " (from VOLCANO_API_URL)"
	}
	return resp, fmt.Errorf(
		"cannot reach the Volcano API at %s%s (%s). Is the server running and is the API URL correct?: %w",
		d.apiURL, source, hint, err,
	)
}

// unreachableHint classifies a transport error into a short human phrase, or ""
// when the error is not a "couldn't reach the server" failure (so it passes
// through unchanged rather than being mislabeled).
func unreachableHint(err error) string {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case isDNSError(err):
		return "host not found"
	case isTimeout(err):
		return "timed out"
	case isOpError(err):
		return "connection failed"
	default:
		return ""
	}
}

func isDNSError(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isOpError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr)
}
