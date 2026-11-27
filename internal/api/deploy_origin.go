package api

import (
	"context"
	"net/http"
	"os"

	"github.com/Kong/volcano-cli/internal/cienv"
)

// attachDeployCIOrigin adds a coarse provenance marker to deployment
// requests. The vendor itself stays in optional CLI telemetry.
func attachDeployCIOrigin(_ context.Context, req *http.Request) error {
	if cienv.Vendor(os.Getenv) != "" {
		req.Header.Set("X-Volcano-Client", "ci")
	}
	return nil
}
