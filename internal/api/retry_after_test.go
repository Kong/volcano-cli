package api

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithRetryAfterReadsSecondsAndDates(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	refusal := &Error{StatusCode: http.StatusTooManyRequests, Message: "limit reached"}

	for value, want := range map[string]time.Time{
		"90":                            now.Add(90 * time.Second),
		" 0 ":                           now,
		"Fri, 23 Oct 2026 15:30:00 GMT": time.Date(2026, 10, 23, 15, 30, 0, 0, time.UTC),
	} {
		err := withRetryAfter(refusal, http.Header{"Retry-After": {value}})
		at, ok := RetryAt(err, now)
		assert.True(t, ok, value)
		assert.True(t, want.Equal(at), "%s: got %s", value, at)
		assert.Equal(t, http.StatusTooManyRequests, Status(err), "the refusal is kept")
		assert.Equal(t, refusal.Error(), err.Error())
	}

	for _, value := range []string{"", "-5", "soon"} {
		err := withRetryAfter(refusal, http.Header{"Retry-After": {value}})
		_, ok := RetryAt(err, now)
		assert.False(t, ok, value)
		assert.Same(t, refusal, err)
	}

	_, ok := RetryAt(errors.New("plain"), now)
	assert.False(t, ok)
}
