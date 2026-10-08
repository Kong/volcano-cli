package localmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// waitForLocalSandboxDeployment bounds cold builds and stops on terminal failures.
func waitForLocalSandboxDeployment(ctx context.Context, interval time.Duration, read func(context.Context) (string, error)) error {
	var output string
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sandbox deployment did not activate: %w\n%s", errors.Join(err, readErr), output)
		}
		output, readErr = read(ctx)
		if readErr == nil {
			var deployment struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(output), &deployment); err != nil {
				return fmt.Errorf("invalid sandbox deployment response: %w", err)
			}
			switch deployment.Status {
			case "active":
				return nil
			case "failed", "retired", "deleted":
				return fmt.Errorf("sandbox deployment reached terminal state %q\n%s", deployment.Status, output)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
}

func TestWaitForLocalSandboxDeployment(t *testing.T) {
	t.Run("polls transient failures and builds until active", func(t *testing.T) {
		calls := 0
		err := waitForLocalSandboxDeployment(t.Context(), 0, func(context.Context) (string, error) {
			calls++
			switch calls {
			case 1:
				return "unavailable", errors.New("temporary read failure")
			case 2:
				return `{"status":"building"}`, nil
			default:
				return `{"status":"active"}`, nil
			}
		})
		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})
	for _, status := range []string{"failed", "retired", "deleted"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			err := waitForLocalSandboxDeployment(t.Context(), time.Hour, func(context.Context) (string, error) {
				calls++
				return fmt.Sprintf(`{"status":%q}`, status), nil
			})
			require.ErrorContains(t, err, "terminal state")
			require.ErrorContains(t, err, status)
			require.Equal(t, 1, calls)
		})
	}
	t.Run("rejects malformed JSON immediately", func(t *testing.T) {
		err := waitForLocalSandboxDeployment(t.Context(), time.Hour, func(context.Context) (string, error) {
			return "not JSON", nil
		})
		require.ErrorContains(t, err, "invalid sandbox deployment response")
	})
	t.Run("cancels without waiting for another poll", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		err := waitForLocalSandboxDeployment(ctx, time.Hour, func(context.Context) (string, error) {
			calls++
			cancel()
			return `{"status":"building"}`, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "building")
		require.Equal(t, 1, calls)
	})
}
