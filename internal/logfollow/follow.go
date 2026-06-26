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

const deploymentPollInterval = 2 * time.Second

// TerminalCheck reports whether a followed deployment has reached a terminal status.
type TerminalCheck func(context.Context) (bool, error)

// CatchUp runs after a followed deployment reaches a terminal status.
type CatchUp func(context.Context, map[string]struct{}) error

// Runtime prints a stream until the stream closes or the context is canceled.
func Runtime(ctx context.Context, w io.Writer, stream *api.ProjectLogStream) error {
	err := output.PrintLogStream(w, stream)
	if cleanStreamShutdown(ctx, err) {
		return nil
	}
	return err
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
