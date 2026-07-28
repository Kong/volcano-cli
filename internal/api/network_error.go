package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// networkErrorDoer turns opaque *dial-phase* transport failures (connection
// refused, host-not-found, could-not-connect) into an actionable message that
// names the API URL being targeted — so a down server or a mis-set API URL
// surfaces as guidance instead of a raw Go dial error like
// `dial tcp 127.0.0.1:8000: connect: connection refused`. It rewrites only
// errors where the request never reached the server; successful requests, HTTP
// error responses, post-connect read/write failures, response timeouts, and all
// other errors pass through untouched, and the original error is preserved.
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
	return resp, fmt.Errorf(
		"cannot reach the Volcano API at %s (%s). Is the server running and is the API URL correct?: %w",
		redactedURL(d.apiURL), hint, err,
	)
}

// unreachableHint classifies a *dial-phase* transport error into a short human
// phrase, or "" when the error is not a "never reached the server" failure (so
// it passes through unchanged rather than being mislabeled). Post-connect
// read/write errors and response/context timeouts deliberately return "": the
// server was reached and may have acted on the request, so claiming it's down
// would be wrong and could hide whether a mutating request ran.
func unreachableHint(err error) string {
	// Connection refused is always a dial-phase failure.
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused"
	}
	// DNS: only a genuine not-found is a clear misconfiguration; resolver
	// timeouts / SERVFAIL are ambiguous and pass through.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "host not found"
		}
		return ""
	}
	// Other dial-phase failures (network unreachable, dial timeout). Restricting
	// to Op == "dial" excludes post-connect read/write OpErrors.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		if opErr.Timeout() {
			return "connection timed out"
		}
		return "could not connect"
	}
	return ""
}

// redactedURL returns the URL verbatim, except it masks an embedded basic-auth
// password (mirroring net/http's own transport-error redaction) so a credential
// baked into the configured API URL never lands in stderr/CI logs.
func redactedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return raw
	}
	return u.Redacted()
}
