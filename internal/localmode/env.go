package localmode

import (
	"github.com/Kong/volcano-cli/internal/envfile"
)

func localEnvOverrides() ([]string, error) {
	return envfile.LoadFirstEnvVars(".env.local")
}
