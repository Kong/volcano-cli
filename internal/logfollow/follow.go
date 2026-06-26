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

// Deployment prints a deployment log stream, stops when terminal returns true,
// then runs catchUp with the IDs already printed from the stream.
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

	for {
		select {
		case err := <-errCh:
			if cleanStreamShutdown(ctx, err) {
				return nil
			}
			return err
		case <-ctx.Done():
			cancel()
			err := <-errCh
			if cleanStreamShutdown(ctx, err) {
				return nil
			}
			return err
		case <-ticker.C():
			done, err := terminal(ctx)
			if err != nil {
				cancel()
				streamErr := <-errCh
				if !cleanStreamShutdown(ctx, streamErr) {
					return streamErr
				}
				return err
			}
			if !done {
				continue
			}
			cancel()
			streamErr := <-errCh
			if !cleanStreamShutdown(ctx, streamErr) {
				return streamErr
			}
			if catchUp == nil {
				return nil
			}
			return catchUp(ctx, printed.snapshot())
		}
	}
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
