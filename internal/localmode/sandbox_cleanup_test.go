package localmode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestStopCleanReclaimsSandboxesBeforeDeletingVolumes(t *testing.T) {
	for _, failAt := range []int{-1, 1, 2, 3, 4, 5, 6, 7, 8} {
		t.Run(map[int]string{-1: "success", 1: "stop", 2: "list-containers", 3: "remove-container", 4: "list-networks", 5: "remove-network", 6: "list-images", 7: "remove-image", 8: "remove-volumes"}[failAt], func(t *testing.T) {
			setLocalDevTestHome(t)
			require.NoError(t, saveDevState(localModeInfo("http://localhost:8000")))
			state, err := DevStatePath()
			require.NoError(t, err)
			step := 0
			runner := &fakeCommandRunner{run: func(_ context.Context, c Command) ([]byte, error) {
				current := step
				step++
				var output string
				switch current {
				case 0:
					require.True(t, commandIs(c, "docker", "inspect", "--format={{.State.Running}}", serverContainerName))
					output = "true"
				case 1:
					require.True(t, commandIsComposeDown(c, false), "stop producers while preserving data")
				case 2:
					require.True(t, commandIs(c, "docker", "container", "ls", "--all", "--quiet", "--filter", "label=dev.volcano.sandbox.namespace=volcano"))
					output = "container123\n"
				case 3:
					require.True(t, commandIs(c, "docker", "container", "rm", "--force", "container123"))
				case 4:
					require.True(t, commandIs(c, "docker", "network", "ls", "--quiet", "--filter", "label=dev.volcano.sandbox.namespace=volcano"))
					output = "network123\n"
				case 5:
					require.True(t, commandIs(c, "docker", "network", "rm", "network123"))
				case 6:
					require.True(t, commandIs(c, "docker", "image", "ls", "--quiet", "--filter", "label=dev.volcano.sandbox.namespace=volcano"))
					output = "image123\nimage123\n"
				case 7:
					require.True(t, commandIs(c, "docker", "image", "rm", "image123"), "never force image deletion")
				case 8:
					require.True(t, commandIsComposeDown(c, true))
				default:
					t.Fatalf("unexpected command: %s", commandDebug(c))
				}
				if current == failAt {
					return nil, errors.New("injected Docker failure")
				}
				return []byte(output), nil
			}}
			var out bytes.Buffer
			err = NewService(cliruntime.Deps{}, WithDockerRunner(runner)).Stop(t.Context(), &out, true)
			if failAt < 0 {
				require.NoError(t, err)
				require.Equal(t, 9, step)
				_, err = os.Stat(state)
				require.True(t, os.IsNotExist(err))
			} else {
				require.ErrorContains(t, err, "injected Docker failure")
				require.Equal(t, failAt+1, step, "failure must stop before deleting more state")
				_, err = os.Stat(state)
				require.NoError(t, err, "retain dev state for recovery")
			}
		})
	}
}
