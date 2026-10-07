package api

import (
	"context"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListVerifiedDomains lists the domains the account has verified.
func (c *Client) ListVerifiedDomains(ctx context.Context) ([]apiclient.VerifiedDomain, error) {
	resp, err := c.client.ListVerifiedDomainsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	result, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON500, resp.JSON501)
	if err != nil {
		return nil, err
	}
	return result.Domains, nil
}

// VerifyDomain proves the account owns domain once DNS serves its record.
// created is false when the account had already verified it.
func (c *Client) VerifyDomain(ctx context.Context, domain string) (*apiclient.VerifiedDomain, bool, error) {
	resp, err := c.client.VerifyDomainWithResponse(ctx, apiclient.VerifyDomainJSONRequestBody{Domain: strings.TrimSpace(domain)})
	if err != nil {
		return nil, false, err
	}
	switch {
	case resp.JSON201 != nil:
		return resp.JSON201, true, nil
	case resp.JSON200 != nil:
		return resp.JSON200, false, nil
	case resp.JSON409 != nil:
		return nil, false, conflictError(resp.StatusCode(), resp.JSON409)
	}
	return nil, false, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body,
		resp.JSON400, resp.JSON401, resp.JSON500, resp.JSON501, resp.JSON503)
}

// DeleteVerifiedDomain gives up the account's ownership of domain.
func (c *Client) DeleteVerifiedDomain(ctx context.Context, domain string) error {
	resp, err := c.client.DeleteVerifiedDomainWithResponse(ctx, strings.TrimSpace(domain))
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON501)
}
