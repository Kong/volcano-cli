package localmode

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// passwordCheckUnavailable is the signup rejection the breached-password check
// returns before any account write. Only it is safe to retry; the window
// matches volcano-hosting's E2E signup retry.
const (
	passwordCheckUnavailable   = "password safety check is temporarily unavailable"
	passwordCheckRetryWindow   = 3 * time.Minute
	passwordCheckRetryInterval = 5 * time.Second
)

// retryLocalModeE2ESignup sends a signup and resends it every interval while
// the breached-password check is unavailable and the next attempt still starts
// inside window. It returns the last answer, so a sustained outage still fails
// the caller.
func retryLocalModeE2ESignup(t *testing.T, window, interval time.Duration, signup func() localModeE2EResponse) localModeE2EResponse {
	t.Helper()
	deadline := time.Now().Add(window)
	resp := signup()
	for passwordCheckWasUnavailable(resp) && time.Now().Add(interval).Before(deadline) {
		t.Logf("retrying auth signup: status %d: %s", resp.status, resp.body)
		time.Sleep(interval)
		resp = signup()
	}
	return resp
}

// passwordCheckWasUnavailable reports whether a signup answer is the
// breached-password check's 503. Other 503s, other statuses, and transport
// failures (status 0) may follow an account write, so they are never retried.
func passwordCheckWasUnavailable(resp localModeE2EResponse) bool {
	if resp.status != http.StatusServiceUnavailable {
		return false
	}
	var body struct {
		Error string `json:"error"`
	}
	return json.Unmarshal([]byte(resp.body), &body) == nil && body.Error == passwordCheckUnavailable
}

func TestPasswordCheckWasUnavailable(t *testing.T) {
	cases := []struct {
		name string
		resp localModeE2EResponse
		want bool
	}{
		{"exact 503", localModeE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable"}`}, true},
		{"exact 503 with other fields", localModeE2EResponse{status: http.StatusServiceUnavailable, body: `{"code":"x","error":"password safety check is temporarily unavailable"}`}, true},
		{"other 503", localModeE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"service unavailable"}`}, false},
		{"503 with longer message", localModeE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable: retry"}`}, false},
		{"503 with non-JSON body", localModeE2EResponse{status: http.StatusServiceUnavailable, body: "password safety check is temporarily unavailable"}, false},
		{"503 with empty body", localModeE2EResponse{status: http.StatusServiceUnavailable}, false},
		{"500 with same body", localModeE2EResponse{status: http.StatusInternalServerError, body: `{"error":"password safety check is temporarily unavailable"}`}, false},
		{"429 with same body", localModeE2EResponse{status: http.StatusTooManyRequests, body: `{"error":"password safety check is temporarily unavailable"}`}, false},
		{"transport failure", localModeE2EResponse{body: "Post \"http://127.0.0.1/auth/signup\": EOF"}, false},
		{"created", localModeE2EResponse{status: http.StatusCreated, body: `{"id":"user"}`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, passwordCheckWasUnavailable(tc.resp))
		})
	}
}

func TestRetryLocalModeE2ESignup(t *testing.T) {
	unavailable := localModeE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable"}`}

	t.Run("retries the password check 503 until signup succeeds", func(t *testing.T) {
		calls := 0
		resp := retryLocalModeE2ESignup(t, time.Hour, 0, func() localModeE2EResponse {
			calls++
			if calls < 3 {
				return unavailable
			}
			return localModeE2EResponse{status: http.StatusCreated, body: `{"id":"user"}`}
		})
		require.Equal(t, http.StatusCreated, resp.status)
		require.Equal(t, 3, calls)
	})
	for name, answer := range map[string]localModeE2EResponse{
		"other 503":         {status: http.StatusServiceUnavailable, body: `{"error":"service unavailable"}`},
		"other status":      {status: http.StatusInternalServerError, body: `{"error":"password safety check is temporarily unavailable"}`},
		"transport failure": {body: "Post \"http://127.0.0.1/auth/signup\": EOF"},
	} {
		t.Run("does not retry "+name, func(t *testing.T) {
			calls := 0
			resp := retryLocalModeE2ESignup(t, time.Hour, 0, func() localModeE2EResponse {
				calls++
				return answer
			})
			require.Equal(t, answer, resp)
			require.Equal(t, 1, calls)
		})
	}
	t.Run("stops when the next attempt would start after the window", func(t *testing.T) {
		calls := 0
		resp := retryLocalModeE2ESignup(t, time.Minute, time.Minute, func() localModeE2EResponse {
			calls++
			return unavailable
		})
		require.Equal(t, unavailable, resp)
		require.Equal(t, 1, calls)
	})
}
