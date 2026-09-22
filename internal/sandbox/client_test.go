package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestDoer func(*http.Request) (*http.Response, error)

func (d requestDoer) Do(r *http.Request) (*http.Response, error) { return d(r) }

func TestRequestHonorsCallerDeadline(t *testing.T) {
	for _, duration := range []time.Duration{30 * time.Second, 5 * time.Minute, 10 * time.Minute} {
		t.Run(duration.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), duration)
			defer cancel()
			want, _ := ctx.Deadline()
			hits := 0
			client, err := New("https://sandbox.example", "secret", "p1", requestDoer(func(r *http.Request) (*http.Response, error) {
				hits++
				got, ok := r.Context().Deadline()
				assert.True(t, ok)
				assert.Equal(t, want, got)
				cancel()
				return nil, r.Context().Err()
			}))
			require.NoError(t, err)
			_, err = client.Do(ctx, Request{Method: http.MethodPost, Path: "/one-shot"})
			require.ErrorIs(t, err, context.Canceled)
			assert.Equal(t, 1, hits)
		})
	}
}

func TestRequestWithoutDeadlineRetainsBoundedFallback(t *testing.T) {
	var requestDone <-chan struct{}
	start := time.Now()
	client, err := New("https://sandbox.example", "secret", "p1", requestDoer(func(r *http.Request) (*http.Response, error) {
		requestDone = r.Context().Done()
		deadline, ok := r.Context().Deadline()
		assert.True(t, ok)
		assert.WithinDuration(t, start.Add(65*time.Second), deadline, time.Second)
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{"X-Volcano-Sandbox-Version": []string{Version}}, Body: http.NoBody}, nil
	}))
	require.NoError(t, err)
	_, err = client.Do(context.Background(), Request{Method: http.MethodGet, Path: "/capabilities"})
	require.NoError(t, err)
	require.NotNil(t, requestDone)
	select {
	case <-requestDone:
	default:
		t.Fatal("fallback deadline was not released after the response")
	}
}

func TestRequestPinsContractIdentityAndNeverReplays(t *testing.T) {
	for _, status := range []int{200, 401, 403, 409, 429, 501, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			hits := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
				assert.Equal(t, Version, r.Header.Get("X-Volcano-Sandbox-Version"))
				assert.Equal(t, "7", r.Header.Get("X-Sandbox-Generation"))
				assert.Equal(t, "request-1", r.Header.Get("Idempotency-Key"))
				assert.Equal(t, "/v1/projects/p1/sandboxes/s1/executions", r.URL.Path)
				w.Header().Set("X-Volcano-Sandbox-Version", Version)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"code":"unknown_outcome","operation_id":"op1","message":"secret"}`)
			}))
			defer server.Close()
			client, err := New(server.URL, "secret", "p1", server.Client())
			require.NoError(t, err)
			_, err = client.Do(t.Context(), Request{Method: http.MethodPost, Path: "/sandboxes/s1/executions", Generation: "7", Key: "request-1", Body: map[string]string{"command": "x"}})
			if status == 200 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "secret")
				assert.Contains(t, err.Error(), "op1")
			}
			assert.Equal(t, 1, hits)
		})
	}
}

func TestRedirectNeverForwardsCredential(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { hits++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client, err := New(origin.URL, "secret", "p1", origin.Client())
	require.NoError(t, err)
	_, err = client.Do(t.Context(), Request{Method: http.MethodPost, Path: "/one-shot"})
	require.Error(t, err)
	assert.Zero(t, hits)
}

func TestVersionAndJSONFailuresDoNotReturnSuccess(t *testing.T) {
	for _, tc := range []struct{ name, version, body string }{
		{"version", "0.1.0", `{}`}, {"missing-version", "", `{}`}, {"json", Version, `{`}, {"trailing", Version, `{} {}`}, {"oversize", Version, `"` + strings.Repeat("x", maxResponseBytes) + `"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Volcano-Sandbox-Version", tc.version)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client, err := New(server.URL, "secret", "p1", server.Client())
			require.NoError(t, err)
			_, err = client.Do(t.Context(), Request{Method: http.MethodGet, Path: "/capabilities"})
			require.Error(t, err)
		})
	}
}

func TestCancellationStopsRequestAndQuietLogStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{true: "logs", false: "request"}[stream], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if stream {
					w.Header().Set("X-Volcano-Sandbox-Version", Version)
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			client, err := New(server.URL, "secret", "p1", server.Client())
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			if stream {
				err = client.Logs(ctx, Request{Method: http.MethodGet, Path: "/sandboxes/s1/logs"}, true, func(json.RawMessage) error { return nil })
			} else {
				_, err = client.Do(ctx, Request{Method: http.MethodPost, Path: "/one-shot"})
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestLogsPreserveFramesAndReportDisconnectCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-Sandbox-Version", Version)
		_, _ = io.WriteString(w, "{\"sequence\":\"9007199254740993\",\"cursor\":\"next\",\"timestamp\":\"2026-09-22T00:00:00Z\",\"truncated\":true}\n")
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", "p1", server.Client())
	require.NoError(t, err)
	var frames []string
	err = client.Logs(t.Context(), Request{Method: http.MethodGet, Path: "/sandboxes/s1/logs"}, true, func(frame json.RawMessage) error { frames = append(frames, string(frame)); return nil })
	require.ErrorContains(t, err, "next")
	require.Len(t, frames, 1)
	assert.Contains(t, frames[0], "9007199254740993")
	assert.Contains(t, frames[0], `"truncated":true`)
}

func TestOriginAndProjectValidation(t *testing.T) {
	for _, origin := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?x=y", "file:///tmp/test"} {
		_, err := New(origin, "secret", "p1", nil)
		require.Error(t, err)
	}
	_, err := New("https://example.com", "secret", "../p1", nil)
	require.Error(t, err)
}
