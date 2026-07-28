package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
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

func TestNetworkErrorDoerLeavesNonNetworkErrorsUntouched(t *testing.T) {
	sentinel := errors.New("some application error")
	d := networkErrorDoer{apiURL: "http://x", next: doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, sentinel
	})}
	_, err := d.Do(mustReq(t))
	require.ErrorIs(t, err, sentinel)
	require.NotContains(t, err.Error(), "cannot reach")
}

func TestUnreachableHintClassifiesTransportErrors(t *testing.T) {
	require.Equal(t, "connection refused", unreachableHint(&net.OpError{Err: syscall.ECONNREFUSED}))
	require.Equal(t, "host not found", unreachableHint(&net.DNSError{Name: "nope.invalid"}))
	require.Equal(t, "connection failed", unreachableHint(&net.OpError{Op: "dial"}))
	require.Empty(t, unreachableHint(errors.New("plain error")))
}
