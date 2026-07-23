package api

import (
	"net/http"
	"testing"
)

func TestRedactHeaderValue(t *testing.T) {
	cases := []struct {
		name   string
		header string
		values []string
		want   string
	}{
		{"authorization shows scheme only", "Authorization", []string{"Bearer sk-secret-token"}, "Bearer <redacted>"},
		{"authorization no scheme", "Authorization", []string{"sk-raw"}, "sk-raw <redacted>"},
		{"authorization absent", "Authorization", []string{""}, "(absent)"},
		{"token header redacted", "X-Api-Token", []string{"abc123"}, "<redacted>"},
		{"api-key redacted", "X-API-Key", []string{"k"}, "<redacted>"},
		{"non-sensitive passthrough", "Content-Type", []string{"application/json"}, "application/json"},
		{"multi value non-sensitive", "Accept", []string{"a", "b"}, "a, b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactHeaderValue(tc.header, tc.values); got != tc.want {
				t.Fatalf("redactHeaderValue(%q, %v) = %q, want %q", tc.header, tc.values, got, tc.want)
			}
		})
	}
}

func TestEnvTruthy(t *testing.T) {
	for _, off := range []string{"", "0", "false", "no", "off", " OFF "} {
		if envTruthy(off) {
			t.Errorf("envTruthy(%q) = true, want false", off)
		}
	}
	for _, on := range []string{"1", "true", "yes", "on", "verbose"} {
		if !envTruthy(on) {
			t.Errorf("envTruthy(%q) = false, want true", on)
		}
	}
}

type recordingDoer struct{ called bool }

func (d *recordingDoer) Do(*http.Request) (*http.Response, error) {
	d.called = true
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK"}, nil
}

func TestDebugDoerPassesThrough(t *testing.T) {
	prev := DebugEnabled()
	t.Cleanup(func() { SetDebug(prev) })

	next := &recordingDoer{}
	doer := debugDoer{next: next}
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8000/x", nil)

	// disabled
	SetDebug(false)
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("unexpected error (disabled): %v", err)
	}
	// enabled (traces to stderr; still passes through and returns the response)
	SetDebug(true)
	resp, err := doer.Do(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected passthrough 200, got resp=%v err=%v", resp, err)
	}
	if !next.called {
		t.Fatal("debugDoer did not call next")
	}
}
