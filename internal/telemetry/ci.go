package telemetry

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

// CIVendor returns the CI vendor id, "generic" when only CI is set, and ""
// outside CI.
func CIVendor(getenv func(string) string) string {
	for _, v := range ciVendors {
		if getenv(v.env) != "" {
			return v.vendor
		}
	}
	if isTruthy(getenv("CI")) {
		return "generic"
	}
	return ""
}
