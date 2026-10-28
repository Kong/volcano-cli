package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

// apiE2EResponse is one API answer. A transport failure has status 0 and the
// error as its body.
type apiE2EResponse struct {
	status int
	body   string
}

// apiE2EPost sends a JSON POST and returns the answer without judging it.
func apiE2EPost(url, token string, body any) apiE2EResponse {
	data, err := json.Marshal(body)
	if err != nil {
		return apiE2EResponse{body: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return apiE2EResponse{body: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiE2EResponse{body: err.Error()}
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return apiE2EResponse{body: err.Error()}
	}
	return apiE2EResponse{status: resp.StatusCode, body: strings.TrimSpace(string(answer))}
}

// retryAPIE2ESignup sends a signup and resends it every interval while the
// breached-password check is unavailable and the next attempt still starts
// inside window. It returns the last answer, so a sustained outage still fails
// the caller.
func retryAPIE2ESignup(t *testing.T, window, interval time.Duration, signup func() apiE2EResponse) apiE2EResponse {
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
func passwordCheckWasUnavailable(resp apiE2EResponse) bool {
	if resp.status != http.StatusServiceUnavailable {
		return false
	}
	var body struct {
		Error string `json:"error"`
	}
	return json.Unmarshal([]byte(resp.body), &body) == nil && body.Error == passwordCheckUnavailable
}

func TestAPIPasswordCheckWasUnavailable(t *testing.T) {
	cases := []struct {
		name string
		resp apiE2EResponse
		want bool
	}{
		{"exact 503", apiE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable"}`}, true},
		{"exact 503 with other fields", apiE2EResponse{status: http.StatusServiceUnavailable, body: `{"code":"x","error":"password safety check is temporarily unavailable"}`}, true},
		{"other 503", apiE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"service unavailable"}`}, false},
		{"503 with longer message", apiE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable: retry"}`}, false},
		{"503 with non-JSON body", apiE2EResponse{status: http.StatusServiceUnavailable, body: "password safety check is temporarily unavailable"}, false},
		{"503 with empty body", apiE2EResponse{status: http.StatusServiceUnavailable}, false},
		{"500 with same body", apiE2EResponse{status: http.StatusInternalServerError, body: `{"error":"password safety check is temporarily unavailable"}`}, false},
		{"transport failure", apiE2EResponse{body: "Post \"http://127.0.0.1/auth/signup\": EOF"}, false},
		{"created", apiE2EResponse{status: http.StatusCreated, body: `{"id":"user"}`}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, passwordCheckWasUnavailable(tc.resp))
		})
	}
}

func TestRetryAPIE2ESignup(t *testing.T) {
	unavailable := apiE2EResponse{status: http.StatusServiceUnavailable, body: `{"error":"password safety check is temporarily unavailable"}`}

	t.Run("retries the password check 503 until signup succeeds", func(t *testing.T) {
		calls := 0
		resp := retryAPIE2ESignup(t, time.Hour, 0, func() apiE2EResponse {
			calls++
			if calls < 3 {
				return unavailable
			}
			return apiE2EResponse{status: http.StatusCreated, body: `{"id":"user"}`}
		})
		require.Equal(t, http.StatusCreated, resp.status)
		require.Equal(t, 3, calls)
	})
	for name, answer := range map[string]apiE2EResponse{
		"other 503":         {status: http.StatusServiceUnavailable, body: `{"error":"service unavailable"}`},
		"other status":      {status: http.StatusInternalServerError, body: `{"error":"password safety check is temporarily unavailable"}`},
		"transport failure": {body: "Post \"http://127.0.0.1/auth/signup\": EOF"},
	} {
		t.Run("does not retry "+name, func(t *testing.T) {
			calls := 0
			resp := retryAPIE2ESignup(t, time.Hour, 0, func() apiE2EResponse {
				calls++
				return answer
			})
			require.Equal(t, answer, resp)
			require.Equal(t, 1, calls)
		})
	}
	t.Run("stops when the next attempt would start after the window", func(t *testing.T) {
		calls := 0
		resp := retryAPIE2ESignup(t, time.Minute, time.Minute, func() apiE2EResponse {
			calls++
			return unavailable
		})
		require.Equal(t, unavailable, resp)
		require.Equal(t, 1, calls)
	})
}
