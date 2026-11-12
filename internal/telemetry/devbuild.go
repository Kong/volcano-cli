package telemetry

import (
	"net/url"
	"strings"

	"github.com/Kong/volcano-cli/internal/version"
)

// devVersion is version.Version in a build that no release stamped: go build,
// go run, or go install without the Makefile's -ldflags.
const devVersion = "dev"

// productionAPIHost is the production API, which forwards usage events and
// feedback reports to the production analytics project.
const productionAPIHost = "api.volcano.dev"

// DevBuildToProduction reports whether this is a development build pointed at
// the production API. Such a build sends no usage events and no feedback
// reports, so a developer's runs and test reports never count as customer
// usage. Pointed at staging or a local server, it sends as usual.
func DevBuildToProduction(apiURL string) bool {
	return version.Version == devVersion && isProductionAPIURL(apiURL)
}

func isProductionAPIURL(apiURL string) bool {
	u, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil {
		return false
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".") == productionAPIHost
}
