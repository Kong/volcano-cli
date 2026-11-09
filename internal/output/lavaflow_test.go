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
	assert.Equal(t, withheld+" (src/cart.go)", lavaflowHoldReason(apiclient.LavaflowHoldReason{Question: withheld, Paths: []string{"", "src/cart.go"}}))
	for _, tc := range []struct {
		name, question, path string
		paths                []string
		want                 bool
	}{
		{name: "exact", question: "src/cart.go", path: "src/cart.go", want: true},
		{name: "ambiguous prose", question: "Changes src/cart.go, which needs review.", path: "src/cart.go"},
		{name: "quoted", question: "Changes `src/cart.go`.", path: "src/cart.go", want: true},
		{name: "ambiguous space in path", question: "Changes config/my settings.yaml, which needs review.", path: "config/my settings.yaml"},
		{name: "ambiguous unicode space", question: "Changes\u00a0src/cart.go.", path: "src/cart.go"},
		{name: "later quoted mention", question: "src/cart.go.map and `src/cart.go`.", path: "src/cart.go", want: true},
		{name: "directory suffix", question: "Changes src/cart.go, which needs review.", path: "cart.go"},
		{name: "filename prefix", question: "Changes src/cart.go.map, which needs review.", path: "src/cart.go"},
		{name: "filename suffix", question: "Changes src/prefix-cart.go, which needs review.", path: "cart.go"},
		{name: "directory prefix", question: "Changes src/cart.go/config.yaml.", path: "src/cart.go"},
		{name: "unicode prefix", question: "Changes café-cart.go.", path: "cart.go"},
		{name: "punctuation in longer name", question: "Changes src/cart.go,backup.", path: "src/cart.go"},
		{name: "empty path", question: "Changes a file.", path: ""},
		{name: "empty question", path: "src/cart.go"},
		{name: "quoted space in path", question: "Changes `reports/daily totals.csv`.", path: "reports/daily totals.csv", want: true},
		{name: "quoted longer spaced path", question: "Changes `reports/daily totals.csv`.", path: "totals.csv"},
		{name: "unquoted longer spaced path", question: "Changes reports/daily totals.csv.", path: "totals.csv"},
		{name: "double quotes", question: `Changes "reports/daily totals.csv".`, path: "reports/daily totals.csv", want: true},
		{name: "single quotes", question: "Changes 'src/cart.go'.", path: "src/cart.go", want: true},
		{name: "embedded double quote", question: "Changes `reports/daily \"totals.csv\" backup`.", path: "totals.csv"},
		{name: "literal embedded quote", question: "Changes `reports/daily \"totals.csv\" backup`.", path: `reports/daily "totals.csv" backup`, want: true},
		{name: "unquoted embedded quote", question: `Changes reports/daily "totals.csv" backup.`, path: "totals.csv", paths: []string{`reports/daily "totals.csv" backup`, "totals.csv"}},
		{name: "unquoted embedded backtick", question: "Changes reports/daily `totals.csv` backup.", path: "totals.csv", paths: []string{"reports/daily `totals.csv` backup", "totals.csv"}},
		{name: "unterminated quote", question: "Changes `src/cart.go", path: "src/cart.go"},
		{name: "later unterminated quote", question: "Changes `src/cart.go` and `other", path: "src/cart.go"},
		{name: "doubled delimiter", question: "Changes ``src/cart.go``.", path: "src/cart.go"},
		{name: "escaped delimiter", question: `Changes "reports/\"totals.csv" backup".`, path: "totals.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			paths := tc.paths
			if paths == nil {
				paths = []string{tc.path}
			}
			assert.Equal(t, tc.want, lavaflowQuestionNamesPath(tc.question, tc.path, paths))
			if !tc.want && tc.path != "" {
				shown := lavaflowHoldReason(apiclient.LavaflowHoldReason{Question: tc.question, Paths: paths})
				assert.Contains(t, shown, clean(tc.path)+")", "an ambiguous match retains the explicit path entry")
			}
		})
	}
}
