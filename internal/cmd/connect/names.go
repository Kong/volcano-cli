package connectcmd

import (
	"fmt"
	"math/rand/v2"
)

// volcanoNames and colorNames are the dashboard's word lists for new project
// names (volcano-web libs/pages/src/lib/generate-project-name.ts), so a
// project connect creates is named like one the dashboard creates. The
// volcanoes leave out those known for deadly eruptions.
var (
	volcanoNames = []string{
		"fuji", "kilauea", "haleakala", "teide", "cotopaxi", "rainier", "shasta",
		"stromboli", "erebus", "taal", "fogo", "arenal", "batur", "bromo",
		"chimborazo", "misti", "popocatepetl", "villarrica", "yasur", "kirishima",
	}
	colorNames = []string{
		"crimson", "amber", "cobalt", "jade", "indigo", "coral", "ochre", "slate",
		"violet", "sienna", "teal", "cerulean", "garnet", "saffron", "magenta",
		"onyx", "topaz", "azure", "umber", "rose",
	}
)

// generateName names a new project when connect has no directory argument and
// no --name. Tests replace it.
var generateName = generateProjectName

// generateProjectName returns a name like fuji-amber-42.
func generateProjectName() string {
	//nolint:gosec // a project name, not a secret
	return fmt.Sprintf("%s-%s-%02d", volcanoNames[rand.IntN(len(volcanoNames))], colorNames[rand.IntN(len(colorNames))], rand.IntN(100))
}
