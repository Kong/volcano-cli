package api

import (
	"bytes"
	"net/http"
	"strings"
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
		{"basic scheme shown", "Authorization", []string{"Basic dXNlcjpwYXNz"}, "Basic <redacted>"},
		{"scheme-less value fully redacted (no leak)", "Authorization", []string{"sk-raw"}, "<redacted>"},
		{"unknown scheme fully redacted", "Authorization", []string{"Weird sk-raw"}, "<redacted>"},
		{"leading whitespace fully redacted", "Authorization", []string{" Bearer sk-raw"}, "<redacted>"},
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
	req, _ := http.NewRequest(http.MethodGet, "http://localhost:8000/x", http.NoBody)

	// disabled: passthrough, and it must not write a trace
	SetDebug(false)
	var quiet bytes.Buffer
	restore := swapDebugOut(&quiet)
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("unexpected error (disabled): %v", err)
	}
	restore()
	if quiet.Len() != 0 {
		t.Fatalf("disabled debug wrote output: %q", quiet.String())
	}
	// enabled: still passes through and returns the response
	SetDebug(true)
	restore = swapDebugOut(&bytes.Buffer{})
	resp, err := doer.Do(req)
	restore()
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("expected passthrough 200, got resp=%v err=%v", resp, err)
	}
	if !next.called {
		t.Fatal("debugDoer did not call next")
	}
}

func TestDebugDoerTraceContract(t *testing.T) {
	prev := DebugEnabled()
	t.Cleanup(func() { SetDebug(prev) })
	SetDebug(true)

	var buf bytes.Buffer
	restore := swapDebugOut(&buf)
	defer restore()

	req, _ := http.NewRequest(http.MethodPost, "http://localhost:8000/auth/device/token", http.NoBody)
	req.Header.Set("Authorization", "Bearer sk-super-secret-token")
	req.Header.Set("X-Api-Token", "raw-token-value")
	req.Header.Set("Content-Type", "application/json")

	doer := debugDoer{next: &recordingDoer{}}
	if _, err := doer.Do(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trace := buf.String()
	// request line + response line present
	assertContains(t, trace, "\u2192 POST http://localhost:8000/auth/device/token")
	assertContains(t, trace, "\u2190 200 OK")
	// sensitive headers redacted; non-sensitive shown
	assertContains(t, trace, "Authorization: Bearer <redacted>")
	assertContains(t, trace, "X-Api-Token: <redacted>")
	assertContains(t, trace, "Content-Type: application/json")
	// the raw credentials must never appear
	if strings.Contains(trace, "sk-super-secret-token") || strings.Contains(trace, "raw-token-value") {
		t.Fatalf("trace leaked a credential:\n%s", trace)
	}
}

func swapDebugOut(w *bytes.Buffer) func() {
	prev := debugOut
	debugOut = w
	return func() { debugOut = prev }
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected trace to contain %q, got:\n%s", needle, haystack)
	}
}
