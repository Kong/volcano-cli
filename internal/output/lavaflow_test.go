package output

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// LavaFlow times print in UTC, marked Z, whatever zone the time carries, so
// they line up with the API's timestamps and a run's log lines.
func TestLavaflowTimesPrintInUTC(t *testing.T) {
	eastern := time.FixedZone("EST", -5*60*60)
	assert.Equal(t, "2026-11-05 22:10Z", formatLavaflowTime(time.Date(2026, 11, 5, 17, 10, 30, 0, eastern)))
	assert.Equal(t, "2026-11-06 03:10Z", formatLavaflowTime(time.Date(2026, 11, 5, 22, 10, 0, 0, eastern)))
	assert.Equal(t, "2026-11-05 22:10Z", formatLavaflowTime(time.Date(2026, 11, 5, 22, 10, 0, 0, time.UTC)))

	at := time.Date(2026, 11, 5, 17, 10, 0, 0, eastern)
	assert.Equal(t, "2026-11-05 22:10Z", formatLavaflowTimePtr(&at, "never"))
	assert.Equal(t, "never", formatLavaflowTimePtr(nil, "never"))
}
