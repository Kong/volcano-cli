package api

import "testing"

// Verifying a domain needs a DNS record this suite cannot publish, so it covers
// what the CLI reports before one exists: the record to publish and each
// refusal. Kong/volcano-hosting's cloud E2E proves a domain through real DNS.
func TestAPIE2ESmokeDomains(t *testing.T) {
	env := setupAPIE2E(t, "smoke-domains")
	env.loginAndUse(t)

	domain := "cli-e2e-" + apiE2ESuffix(t) + ".example.com"
	env.runCloudCLI(t, "domains", "verify", domain).requireFailure(t,
		"ownership of "+domain+" is not verified",
		"Publish this DNS record",
		"_volcano."+domain+"  TXT  \"volcano-domain-verification=",
	)

	listed := env.runCloudCLI(t, "domains", "list")
	listed.requireSuccess(t)
	listed.requireNotContains(t, domain)

	env.runCloudCLI(t, "domains", "remove", domain, "--yes").requireFailure(t, "HTTP 404")
	env.runCloudCLI(t, "domains", "verify", "co.uk").requireFailure(t, "HTTP 400")
}
