package api

import (
	"errors"
	"net/http"
)

// GrantBudgetExhaustedCode is the API's 429 code for a change made after a
// delegated grant used all of its mutations. A new grant gets a new budget.
const GrantBudgetExhaustedCode = "grant_budget_exhausted"

// GrantBudgetExhausted reports whether err is the API's 429 for a delegated
// grant with no mutations left.
func GrantBudgetExhausted(err error) bool {
	apiErr, ok := errors.AsType[*Error](err)
	return ok && apiErr.StatusCode == http.StatusTooManyRequests && apiErr.Code == GrantBudgetExhaustedCode
}
