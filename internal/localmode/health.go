package localmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func (s Service) waitForServer(ctx context.Context, w io.Writer) error {
	waitCtx, cancel := context.WithTimeout(ctx, s.waitTimeout)
	defer cancel()

	fmt.Fprint(w, "Waiting for server")
	for {
		if err := s.checkHealth(waitCtx); err == nil {
			fmt.Fprintln(w)
			return nil
		}
		if err := waitCtx.Err(); err != nil {
			fmt.Fprintln(w)
			if errors.Is(err, context.DeadlineExceeded) {
				return errors.New("timeout waiting for server to be ready")
			}
			return err
		}
		fmt.Fprint(w, ".")

		timer := time.NewTimer(s.pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			fmt.Fprintln(w)
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return errors.New("timeout waiting for server to be ready")
			}
			return waitCtx.Err()
		case <-timer.C:
		}
	}
}

func (s Service) checkHealth(ctx context.Context) error {
	healthCtx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, s.healthURL, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := s.healthClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func defaultDialTCP(ctx context.Context, address string) error {
	dialer := &net.Dialer{Timeout: healthTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}
