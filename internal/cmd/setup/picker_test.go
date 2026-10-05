package setupcmd

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// TestPickerShowsEveryOption pins the workaround for charmbracelet/huh#831:
// a huh MultiSelect inside a Form with no explicit Height collapses its
// viewport to N-2 rows, hiding every option at N<=2 and clipping the last
// two at larger N. newHarnessPicker passes Height(len(options)+2) to force
// the non-buggy code path; this test fails if that call is dropped or if
// huh regresses the fix.
func TestPickerShowsEveryOption(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			options := make([]huh.Option[string], n)
			for i := range options {
				name := fmt.Sprintf("harness-%d", i)
				options[i] = huh.NewOption("[available] "+name, name).Selected(true)
			}

			var selected []string
			form := newHarnessPicker(options, &selected, false)
			_ = form.Init()
			_, _ = form.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

			view := form.View()
			for i := range n {
				want := fmt.Sprintf("harness-%d", i)
				if !strings.Contains(view, want) {
					t.Errorf("picker view missing %q (n=%d)\n--- view ---\n%s\n--- end ---", want, n, view)
				}
			}
		})
	}
}
