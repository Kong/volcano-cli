package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryLaterError is a refusal whose response said when to try again, in its
// Retry-After header.
type RetryLaterError struct {
	Err error
	// RetryAfter is the header's value: a number of seconds or an HTTP date.
	RetryAfter string
}

func (e *RetryLaterError) Error() string { return e.Err.Error() }

func (e *RetryLaterError) Unwrap() error { return e.Err }

// RetryAt returns when the refusal wrapped in err said to try again, counting
// a wait in seconds from now, and false when it did not say.
func RetryAt(err error, now time.Time) (time.Time, bool) {
	retry, ok := errors.AsType[*RetryLaterError](err)
	if !ok {
		return time.Time{}, false
	}
	return parseRetryAfter(retry.RetryAfter, now)
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	if at, err := http.ParseTime(value); err == nil {
		return at, true
	}
	return time.Time{}, false
}

// withRetryAfter keeps a response's Retry-After with err, when it has one this
// package can read.
func withRetryAfter(err error, header http.Header) error {
	value := header.Get("Retry-After")
	if _, ok := parseRetryAfter(value, time.Time{}); !ok {
		return err
	}
	return &RetryLaterError{Err: err, RetryAfter: value}
}
