package logfollow_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		_, _ = w.Write([]byte(`data: {"id":"streamed-id","message":"streamed log","timestamp":1760000000000,"resource":{"type":"function","id":"` + functionID.String() + `"}}` + "\n\n"))
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
	var out bytes.Buffer
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
