package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/version"
)

// maxPayloadBytes matches the server's body limit for POST /cli/telemetry.
const maxPayloadBytes = 16 << 10

// flushTimeout bounds the detached flush process's one HTTP request.
const flushTimeout = 5 * time.Second

// spawnFlush hands payload to a detached `volcano telemetry flush` process, so
// the command that produced it exits without waiting on the network.
func spawnFlush(payload []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Background context: the child must outlive this process, so nothing may
	// cancel it.
	cmd := exec.CommandContext(context.Background(), exe, "telemetry", "flush") //nolint:gosec // re-runs this same binary with fixed args
	// Run from the temp dir, not the caller's working directory, so the flush
	// never holds open a directory the user may be about to delete (Windows).
	cmd.Dir = os.TempDir()
	detach(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The payload fits in the pipe buffer, so this returns before the child
	// reads it.
	_, writeErr := stdin.Write(payload)
	closeErr := stdin.Close()
	releaseErr := cmd.Process.Release()
	return errors.Join(writeErr, closeErr, releaseErr)
}

// Flush reads one event payload from r and posts it to the cloud API's
// /cli/telemetry endpoint, with the saved login token when there is one. The
// server accepts the event without a token.
func Flush(ctx context.Context, r io.Reader, client *http.Client) error {
	payload, err := io.ReadAll(io.LimitReader(r, maxPayloadBytes+1))
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maxPayloadBytes {
		return fmt.Errorf("telemetry payload is empty or over %d bytes", maxPayloadBytes)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if client == nil {
		client = &http.Client{Timeout: flushTimeout}
	}
	url := strings.TrimRight(cfg.APIURL(), "/") + "/cli/telemetry"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "volcano-cli/"+version.Version)
	if token := cfg.Token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}
