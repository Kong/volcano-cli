package api

import (
	"errors"
	"net/http"
)

// Codes the API answers a pg- delegated grant with. Each refusal carries one,
// so a caller can tell them apart without parsing the message.
const (
	// GrantRouteNotAllowedCode is a 403 for a route outside what any delegated
	// grant may call.
	GrantRouteNotAllowedCode = "grant_route_not_allowed"
	// GrantPermissionMissingCode is a 403 for a route the grant may call only
	// with a permission it does not carry.
	GrantPermissionMissingCode = "grant_permission_missing"
	// GrantBudgetExhaustedCode is a 429 for a change made after the grant used
	// all of its mutations. The grant still works for reads; a new grant gets a
	// new budget.
	GrantBudgetExhaustedCode = "grant_budget_exhausted"
)

// GrantBudgetExhausted reports whether err is the API's 429 for a delegated
// grant with no mutations left.
func GrantBudgetExhausted(err error) bool {
	apiErr, ok := errors.AsType[*Error](err)
	return ok && apiErr.StatusCode == http.StatusTooManyRequests && apiErr.Code == GrantBudgetExhaustedCode
}
