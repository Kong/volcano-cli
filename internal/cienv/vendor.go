// Package cienv classifies the CLI invocation environment.
package cienv

import "strings"

// ciVendors maps a CI vendor's own environment variable to the vendor id sent
// as ci_vendor. Order matters only when several are set; the first wins.
var ciVendors = []struct {
	env     string
	vendor  string
	matches func(string) bool
}{
	{"GITHUB_ACTIONS", "github-actions", enabled},
	{"GITLAB_CI", "gitlab", enabled},
	{"CIRCLECI", "circleci", enabled},
	{"BUILDKITE", "buildkite", enabled},
	{"JENKINS_URL", "jenkins", present},
	{"TRAVIS", "travis", enabled},
	{"TF_BUILD", "azure-pipelines", enabled},
	{"BITBUCKET_BUILD_NUMBER", "bitbucket", present},
	{"TEAMCITY_VERSION", "teamcity", present},
	{"CODEBUILD_BUILD_ID", "codebuild", present},
	{"VERCEL", "vercel", enabled},
	{"NETLIFY", "netlify", enabled},
	{"DRONE", "drone", enabled},
	{"SEMAPHORE", "semaphore", enabled},
	{"APPVEYOR", "appveyor", enabled},
	{"BUILDER_OUTPUT", "google-cloud-build", present},
}

// Vendor returns the CI vendor ID, "generic" when only CI is set, and ""
// outside CI.
func Vendor(getenv func(string) string) string {
	for _, v := range ciVendors {
		if v.matches(getenv(v.env)) {
			return v.vendor
		}
	}
	if enabled(getenv("CI")) {
		return "generic"
	}
	return ""
}

// A vendor can advertise a boolean flag or identify a build by a URL, path,
// version or number. Only flags interpret false-like values as disabled.
func present(value string) bool { return strings.TrimSpace(value) != "" }

func enabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
