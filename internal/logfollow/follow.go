// Package logfollow contains shared CLI log stream follow loops.
package logfollow

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	deploymentPollInterval = 2 * time.Second
	reconnectBackoff       = 1 * time.Second
)

// TerminalCheck reports whether a followed deployment has reached a terminal status.
type TerminalCheck func(context.Context) (bool, error)

// CatchUp runs after a followed deployment reaches a terminal status.
type CatchUp func(context.Context, map[string]struct{}) error

// StreamOpener opens a project log stream, resuming after lastEventID when it is
// set. logfollow re-invokes it to reconnect a dropped stream from its last
// cursor.
type StreamOpener func(ctx context.Context, lastEventID string) (*api.ProjectLogStream, error)

// Runtime follows a runtime log stream until the context is canceled. The
// backend holds a healthy connection open and tails new events, so a closed
// stream is treated as a transient disconnect: Runtime reconnects from the last
// stream cursor — resuming without replaying recent events — until the context
// is canceled. A failure to open the stream is surfaced to the caller.
func Runtime(ctx context.Context, deps cliruntime.Deps, w io.Writer, open StreamOpener) error {
	stream := newReconnectingStream(ctx, deps, open)
	defer func() {
		_ = stream.Close()
	}()
	for {
		event, err := stream.Next()
		if err != nil {
			if cleanStreamShutdown(ctx, err) {
				return nil
			}
			return err
		}
		output.PrintLogStreamEvent(w, event)
	}
}

// Deployment prints a deployment log stream until the deployment reaches a
// terminal status, then runs catchUp with the IDs already printed from the
// stream. If the stream closes before the deployment is terminal, it keeps
// polling so the catch-up search still runs.
func Deployment(ctx context.Context, deps cliruntime.Deps, w io.Writer, stream *api.ProjectLogStream, cancel context.CancelFunc, terminal TerminalCheck, catchUp CatchUp) error {
	if cancel == nil {
		cancel = func() {}
	}
	defer cancel()

	printed := newPrintedLogIDs()
	errCh := make(chan error, 1)
	go func() {
		defer func() {
			_ = stream.Close()
		}()
		for {
			event, err := stream.Next()
			if err != nil {
				errCh <- err
				return
			}
			if event.Log != nil {
				printed.add(event.Log.Id)
			}
			output.PrintLogStreamEvent(w, event)
		}
	}()

	ticker := cliruntime.NewTicker(deps, deploymentPollInterval)
	defer ticker.Stop()

	// streamCh is set to nil once the stream goroutine has exited so the select
	// stops waiting on it; the loop then keeps polling for terminal status.
	var streamCh <-chan error = errCh

	// finish stops the stream goroutine (if still running) and runs the
	// duplicate-suppressed catch-up search.
	finish := func() error {
		cancel()
		if streamErr := awaitStream(streamCh); !cleanStreamShutdown(ctx, streamErr) {
			return streamErr
		}
		if catchUp == nil {
			return nil
		}
		return catchUp(ctx, printed.snapshot())
	}

	for {
		select {
		case err := <-streamCh:
			// The stream ended on its own. Surface unexpected failures; on a
			// clean shutdown stop waiting on the stream and check terminal status
			// now so the catch-up search runs without waiting for the next poll —
			// the stream can close before the deployment is terminal.
			if !cleanStreamShutdown(ctx, err) {
				return err
			}
			streamCh = nil
			done, err := terminal(ctx)
			if err != nil {
				return err
			}
			if done {
				return finish()
			}
		case <-ctx.Done():
			cancel()
			if err := awaitStream(streamCh); !cleanStreamShutdown(ctx, err) {
				return err
			}
			return nil
		case <-ticker.C():
			done, err := terminal(ctx)
			if err != nil {
				cancel()
				if streamErr := awaitStream(streamCh); !cleanStreamShutdown(ctx, streamErr) {
					return streamErr
				}
				return err
			}
			if !done {
				continue
			}
			return finish()
		}
	}
}

// awaitStream waits for the streaming goroutine to send its final error,
// returning nil if it has already exited (streamCh is nil).
func awaitStream(streamCh <-chan error) error {
	if streamCh == nil {
		return nil
	}
	return <-streamCh
}

func cleanStreamShutdown(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	return ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)
}

// reconnectingStream reads project log stream events, reconnecting from the last
// delivered cursor when the underlying stream ends. A failure to (re)open the
// stream is surfaced to the caller; a mid-stream read failure backs off and
// reconnects so the tail survives a dropped connection.
type reconnectingStream struct {
	ctx    context.Context
	deps   cliruntime.Deps
	open   StreamOpener
	stream *api.ProjectLogStream
	lastID string
}

func newReconnectingStream(ctx context.Context, deps cliruntime.Deps, open StreamOpener) *reconnectingStream {
	return &reconnectingStream{ctx: ctx, deps: deps, open: open}
}

// Next returns the next stream event, transparently reconnecting from the last
// cursor when the stream drops. It only returns an error when the context is
// canceled or the stream cannot be opened.
func (s *reconnectingStream) Next() (*api.ProjectLogStreamEvent, error) {
	for {
		if s.stream == nil {
			stream, err := s.open(s.ctx, s.lastID)
			if err != nil {
				return nil, err
			}
			s.stream = stream
		}
		event, err := s.stream.Next()
		if err != nil {
			_ = s.stream.Close()
			s.stream = nil
			if s.ctx.Err() != nil {
				return nil, err
			}
			// A healthy backend stream stays open, so any close is a
			// disconnect: back off and reconnect from the last cursor.
			if waitErr := s.wait(); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		if event != nil && event.ID != "" {
			s.lastID = event.ID
		}
		return event, nil
	}
}

// Close closes the current underlying stream, if any.
func (s *reconnectingStream) Close() error {
	if s.stream == nil {
		return nil
	}
	err := s.stream.Close()
	s.stream = nil
	return err
}

func (s *reconnectingStream) wait() error {
	ticker := cliruntime.NewTicker(s.deps, reconnectBackoff)
	defer ticker.Stop()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-ticker.C():
		return nil
	}
}

type printedLogIDs struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func newPrintedLogIDs() *printedLogIDs {
	return &printedLogIDs{ids: make(map[string]struct{})}
}

func (p *printedLogIDs) add(id string) {
	if id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ids[id] = struct{}{}
}

func (p *printedLogIDs) snapshot() map[string]struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := make(map[string]struct{}, len(p.ids))
	for id := range p.ids {
		snapshot[id] = struct{}{}
	}
	return snapshot
}
