// Package cienv classifies the CLI invocation environment.
package cienv

import "strings"

// ciVendors maps a CI vendor's own environment variable to the vendor id sent
// as ci_vendor. Order matters only when several are set; the first wins.
var ciVendors = []struct {
	env    string
	vendor string
}{
	{"GITHUB_ACTIONS", "github-actions"},
	{"GITLAB_CI", "gitlab"},
	{"CIRCLECI", "circleci"},
	{"BUILDKITE", "buildkite"},
	{"JENKINS_URL", "jenkins"},
	{"TRAVIS", "travis"},
	{"TF_BUILD", "azure-pipelines"},
	{"BITBUCKET_BUILD_NUMBER", "bitbucket"},
	{"TEAMCITY_VERSION", "teamcity"},
	{"CODEBUILD_BUILD_ID", "codebuild"},
	{"VERCEL", "vercel"},
	{"NETLIFY", "netlify"},
	{"DRONE", "drone"},
	{"SEMAPHORE", "semaphore"},
	{"APPVEYOR", "appveyor"},
	{"BUILDER_OUTPUT", "google-cloud-build"},
}

// Vendor returns the CI vendor ID, "generic" when only CI is set, and ""
// outside CI.
func Vendor(getenv func(string) string) string {
	for _, v := range ciVendors {
		if getenv(v.env) != "" {
			return v.vendor
		}
	}
	switch strings.ToLower(strings.TrimSpace(getenv("CI"))) {
	case "", "0", "false", "no", "off":
		return ""
	default:
		return "generic"
	}
}
