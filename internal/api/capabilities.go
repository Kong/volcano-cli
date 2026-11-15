package api

import (
	"context"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// CapabilityAvailable reports whether the API lists capability id as
// available. The list is how a client learns what an environment offers, so a
// capability it leaves out, or reports with any status but available, is
// unavailable.
func (c *Client) CapabilityAvailable(ctx context.Context, id string) (bool, error) {
	resp, err := c.client.ListCapabilitiesWithResponse(ctx)
	if err != nil {
		return false, err
	}
	list, err := apiResult[apiclient.CapabilityList](resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return false, err
	}
	for _, capability := range list.Data {
		if capability.Id == id {
			return capability.Status == apiclient.CapabilityStatusAvailable, nil
		}
	}
	return false, nil
}
