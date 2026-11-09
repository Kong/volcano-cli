package output

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Kong/volcano-cli/internal/apiclient"
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

func TestLavaflowHoldPathsRequireCompleteMentions(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Review this change.", lavaflowHoldReason(apiclient.LavaflowHoldReason{Question: "Review this change.", Paths: []string{""}}))
	assert.Equal(t, `Review this change. (src/\x1b[2J.go)`, lavaflowHoldReason(apiclient.LavaflowHoldReason{Question: "Review this change.", Paths: []string{"src/\x1b[2J.go"}}))
	const withheld = "LavaFlow left this path out because it may contain a credential."
	assert.Equal(t, withheld, lavaflowHoldReason(apiclient.LavaflowHoldReason{Question: withheld}))
	for _, tc := range []struct {
		name, question, path string
		want                 bool
	}{
		{name: "exact", question: "src/cart.go", path: "src/cart.go", want: true},
		{name: "prose", question: "Changes src/cart.go, which needs review.", path: "src/cart.go", want: true},
		{name: "quoted", question: "Changes `src/cart.go`.", path: "src/cart.go", want: true},
		{name: "space in path", question: "Changes config/my settings.yaml, which needs review.", path: "config/my settings.yaml", want: true},
		{name: "unicode space", question: "Changes\u00a0src/cart.go.", path: "src/cart.go", want: true},
		{name: "later complete mention", question: "src/cart.go.map and src/cart.go.", path: "src/cart.go", want: true},
		{name: "directory suffix", question: "Changes src/cart.go, which needs review.", path: "cart.go"},
		{name: "filename prefix", question: "Changes src/cart.go.map, which needs review.", path: "src/cart.go"},
		{name: "filename suffix", question: "Changes src/prefix-cart.go, which needs review.", path: "cart.go"},
		{name: "directory prefix", question: "Changes src/cart.go/config.yaml.", path: "src/cart.go"},
		{name: "unicode prefix", question: "Changes café-cart.go.", path: "cart.go"},
		{name: "punctuation in longer name", question: "Changes src/cart.go,backup.", path: "src/cart.go"},
		{name: "empty path", question: "Changes a file.", path: ""},
		{name: "empty question", path: "src/cart.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, lavaflowQuestionNamesPath(tc.question, tc.path))
		})
	}
}
