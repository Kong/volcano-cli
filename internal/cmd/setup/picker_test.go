package setupcmd

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Kong/volcano-cli/internal/setup"
)

// TestPickerShowsEveryOption checks that wrapped headings do not hide options.
func TestPickerShowsEveryOption(t *testing.T) {
	for _, width := range []int{120, 40} {
		for _, color := range []bool{false, true} {
			for _, n := range []int{1, 2, 3, 5} {
				t.Run(fmt.Sprintf("width=%d/color=%t/n=%d", width, color, n), func(t *testing.T) {
					options := make([]huh.Option[string], n)
					for i := range options {
						name := fmt.Sprintf("harness-%d", i)
						options[i] = huh.NewOption("[available] "+name, name).Selected(true)
					}

					var selected []string
					form := newHarnessPicker(options, &selected, color)
					_ = form.Init()
					_, _ = form.Update(tea.WindowSizeMsg{Width: width, Height: 30})

					view := form.View()
					plain := strings.Join(strings.Fields(ansi.Strip(view)), " ")
					for _, want := range []string{"Install Volcano for which coding agents?", keyHintDescription(false)} {
						if !strings.Contains(plain, want) {
							t.Errorf("picker heading missing %q: %s", want, plain)
						}
					}
					if color && width == 120 {
						title := lipgloss.NewStyle().Foreground(lipgloss.Color(setup.LavaHex)).Bold(true).
							Render("Install Volcano for which coding agents?")
						if !strings.Contains(view, title) {
							t.Errorf("picker title lost its lava color or bold style: %q", view)
						}
					}
					for i := range n {
						want := fmt.Sprintf("harness-%d", i)
						if !strings.Contains(view, want) {
							t.Errorf("picker view missing %q\n--- view ---\n%s\n--- end ---", want, view)
						}
					}
				})
			}
		}
	}
}
