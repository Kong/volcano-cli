package logfollow_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/logfollow"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDeploymentStopsAtTerminalStatusAndRunsCatchUp(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	functionID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: stream-cursor\n"))
		_, _ = w.Write([]byte("event: log\n"))
		_, _ = w.Write([]byte(`data: {"id":"streamed-id","body":"streamed log","timestamp":"2025-10-09T08:53:20Z","resource":{"type":"function","id":"` + functionID.String() + `"}}` + "\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", api.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	ctx := context.Background()
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := client.StreamFunctionLogs(streamCtx, projectID, functionID, 100, "")
	require.NoError(t, err)

	ticker := newFakeTicker()
	var out syncBuffer
	var printed map[string]struct{}
	done := make(chan error, 1)
	go func() {
		done <- logfollow.Deployment(ctx, cliruntime.Deps{
			NewTicker: func(time.Duration) cliruntime.Ticker {
				return ticker
			},
		}, &out, stream, cancel, func(context.Context) (bool, error) {
			return true, nil
		}, func(_ context.Context, ids map[string]struct{}) error {
			printed = ids
			return nil
		})
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "streamed log")
	}, time.Second, 10*time.Millisecond)
	ticker.tick()

	require.NoError(t, <-done)
	assert.Contains(t, printed, "streamed-id")
}

func TestDeploymentRunsCatchUpWhenStreamEndsBeforeTerminal(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	functionID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	// The server delivers one event and then closes the stream (EOF) while the
	// deployment is still building.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: stream-cursor\n"))
		_, _ = w.Write([]byte("event: log\n"))
		_, _ = w.Write([]byte(`data: {"id":"streamed-id","body":"streamed log","timestamp":"2025-10-09T08:53:20Z","resource":{"type":"function","id":"` + functionID.String() + `"}}` + "\n\n"))
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", api.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	ctx := context.Background()
	streamCtx, cancel := context.WithCancel(ctx)
	stream, err := client.StreamFunctionLogs(streamCtx, projectID, functionID, 100, "")
	require.NoError(t, err)

	ticker := newFakeTicker()
	var out syncBuffer
	terminalReady := make(chan struct{})
	var printed map[string]struct{}
	done := make(chan error, 1)
	go func() {
		done <- logfollow.Deployment(ctx, cliruntime.Deps{
			NewTicker: func(time.Duration) cliruntime.Ticker {
				return ticker
			},
		}, &out, stream, cancel, func(context.Context) (bool, error) {
			select {
			case <-terminalReady:
				return true, nil
			default:
				return false, nil
			}
		}, func(_ context.Context, ids map[string]struct{}) error {
			printed = ids
			return nil
		})
	}()

	// Wait until the streamed event is printed; the stream then closes on its
	// own, but the follow loop must keep polling instead of exiting.
	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "streamed log")
	}, time.Second, 10*time.Millisecond)

	// Mark the deployment terminal and drive the poll ticker until the loop
	// returns. The catch-up search must still run even though the stream ended.
	close(terminalReady)
	for {
		select {
		case followErr := <-done:
			require.NoError(t, followErr)
			assert.Contains(t, printed, "streamed-id")
			return
		case ticker.ch <- time.Now():
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestRuntimeReconnectsFromCursor(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	functionID := uuid.MustParse("22222222-2222-4222-8222-222222222222")

	var mu sync.Mutex
	var lastEventIDs []string
	// The first connection delivers one event and then closes (EOF) while the
	// resource keeps running; the resumed connection delivers a second event and
	// stays open like a healthy tail.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt := len(lastEventIDs)
		lastEventIDs = append(lastEventIDs, r.Header.Get("Last-Event-ID"))
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if attempt == 0 {
			writeStreamLog(w, "cursor-1", "first-id", "first event")
			return
		}
		writeStreamLog(w, "cursor-2", "second-id", "second event")
		<-r.Context().Done()
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", api.WithHTTPClient(server.Client()))
	require.NoError(t, err)

	open := func(ctx context.Context, lastEventID string) (*api.ProjectLogStream, error) {
		return client.StreamFunctionLogs(ctx, projectID, functionID, 100, lastEventID)
	}

	ticker := newFakeTicker()
	deps := cliruntime.Deps{NewTicker: func(time.Duration) cliruntime.Ticker { return ticker }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- logfollow.Runtime(ctx, deps, &out, open)
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "first event")
	}, time.Second, 10*time.Millisecond)

	// Release the reconnect backoff until the resumed event arrives.
	require.Eventually(t, func() bool {
		select {
		case ticker.ch <- time.Now():
		default:
		}
		return strings.Contains(out.String(), "second event")
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.NoError(t, <-done)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(lastEventIDs), 2)
	assert.Empty(t, lastEventIDs[0])
	assert.Equal(t, "cursor-1", lastEventIDs[1])
}

func TestRuntimeSurfacesOpenError(t *testing.T) {
	wantErr := errors.New("open failed")
	open := func(context.Context, string) (*api.ProjectLogStream, error) {
		return nil, wantErr
	}
	err := logfollow.Runtime(context.Background(), cliruntime.Deps{}, &syncBuffer{}, open)
	require.ErrorIs(t, err, wantErr)
}

func writeStreamLog(w http.ResponseWriter, cursor, logID, body string) {
	_, _ = w.Write([]byte("id: " + cursor + "\n"))
	_, _ = w.Write([]byte("event: log\n"))
	_, _ = w.Write([]byte(`data: {"id":"` + logID + `","body":"` + body + `","timestamp":"2025-10-09T08:53:20Z","resource":{"type":"function","id":"22222222-2222-4222-8222-222222222222"}}` + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 1)}
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.ch
}

func (t *fakeTicker) Reset(time.Duration) {}

func (t *fakeTicker) Stop() {}

func (t *fakeTicker) tick() {
	t.ch <- time.Now()
}
