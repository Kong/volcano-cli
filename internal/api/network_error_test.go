package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

// doerFunc is defined in version_protocol_test.go (same package).

func mustReq(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://localhost:8000/projects", http.NoBody)
	require.NoError(t, err)
	return req
}

func TestNetworkErrorDoerWrapsConnectionRefused(t *testing.T) {
	refused := &url.Error{Op: "Get", URL: "http://localhost:8000/projects", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED,
	}}
	d := networkErrorDoer{apiURL: "http://localhost:8000", next: doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, refused
	})}

	_, err := d.Do(mustReq(t))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot reach the Volcano API at http://localhost:8000")
	require.Contains(t, err.Error(), "connection refused")
	require.ErrorIs(t, err, syscall.ECONNREFUSED) // original preserved
}

func TestNetworkErrorDoerPassesThroughHTTPResponses(t *testing.T) {
	d := networkErrorDoer{apiURL: "http://x", next: doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody}, nil
	})}
	resp, err := d.Do(mustReq(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestNetworkErrorDoerLeavesNonDialErrorsUntouched(t *testing.T) {
	// A post-connect read failure: the server WAS reached, so it must not be
	// rewritten as "cannot reach".
	readReset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	d := networkErrorDoer{apiURL: "http://x", next: doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, readReset
	})}
	_, err := d.Do(mustReq(t))
	require.ErrorIs(t, err, readReset)
	require.NotContains(t, err.Error(), "cannot reach")
}

func TestNetworkErrorDoerRedactsEmbeddedPassword(t *testing.T) {
	refused := &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	d := networkErrorDoer{apiURL: "https://user:s3cret@api.example.com", next: doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, refused
	})}
	_, err := d.Do(mustReq(t))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "s3cret")
	require.Contains(t, err.Error(), "user:xxxxx@api.example.com")
}

func TestUnreachableHintOnlyMatchesDialPhase(t *testing.T) {
	timeoutDial := &net.OpError{Op: "dial", Err: os.ErrDeadlineExceeded}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"connection refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "connection refused"},
		{"host not found", &net.DNSError{Name: "nope.invalid", IsNotFound: true}, "host not found"},
		{"dial failure", &net.OpError{Op: "dial", Net: "tcp"}, "could not connect"},
		{"dial timeout", timeoutDial, "connection timed out"},
		// Pass-through: server was reached / ambiguous.
		{"post-connect read", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, ""},
		{"dns non-notfound", &net.DNSError{Name: "x", IsTimeout: true}, ""},
		{"plain error", errors.New("boom"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, unreachableHint(tc.err))
		})
	}
}
