package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

const maxCallbackTokenLength = 128

// callbackToken is the shape the API accepts. A token is checked here so a
// malformed one is never sent: a validation error could quote it back, and
// error text reaches usage telemetry.
var callbackToken = regexp.MustCompile(`^vcb_[A-Za-z0-9_-]+$`)

// ValidateCallbackToken refuses a value that cannot be a callback token. The
// error never quotes the value, which may be a token with a typo in it.
func ValidateCallbackToken(token string) error {
	if len(token) > maxCallbackTokenLength || !callbackToken.MatchString(token) {
		return fmt.Errorf("invalid callback token: a token is vcb_ followed by letters, digits, '-' or '_', "+
			"up to %d characters in all", maxCallbackTokenLength)
	}
	return nil
}

// SucceedCallback resumes the step waiting on token with result.
func (s Service) SucceedCallback(
	ctx context.Context, token string, result json.RawMessage,
) (*apiclient.DurableCallbackOutcome, error) {
	client, err := s.callbackClient(token)
	if err != nil {
		return nil, err
	}
	outcome, err := client.SucceedDurableCallback(ctx, token, result)
	if err != nil {
		return nil, callbackError("succeed", err)
	}
	return outcome, nil
}

// FailCallback resumes the step waiting on token by raising an error with
// errorType and message.
func (s Service) FailCallback(
	ctx context.Context, token, errorType, message string,
) (*apiclient.DurableCallbackOutcome, error) {
	client, err := s.callbackClient(token)
	if err != nil {
		return nil, err
	}
	outcome, err := client.FailDurableCallback(ctx, token, errorType, message)
	if err != nil {
		return nil, callbackError("fail", err)
	}
	return outcome, nil
}

// HeartbeatCallback restarts the heartbeat timeout of the step waiting on
// token.
func (s Service) HeartbeatCallback(ctx context.Context, token string) (*apiclient.DurableCallbackOutcome, error) {
	client, err := s.callbackClient(token)
	if err != nil {
		return nil, err
	}
	outcome, err := client.HeartbeatDurableCallback(ctx, token)
	if err != nil {
		if api.Status(err) == http.StatusServiceUnavailable {
			return nil, api.WithCause(errors.New("the heartbeat could not reach the execution right now; "+
				"the wait is unchanged, so retry with backoff"), err)
		}
		return nil, callbackError("heartbeat", err)
	}
	return outcome, nil
}

// callbackClient sends no credential: the token is the only one the callback
// routes take, so answering a callback needs no login or project. That is what
// lets the system a workflow handed the token to run these commands.
func (s Service) callbackClient(token string) (*api.Client, error) {
	if err := ValidateCallbackToken(token); err != nil {
		return nil, err
	}
	cfg, err := s.sessions.Config()
	if err != nil {
		return nil, err
	}
	client, err := s.sessions.APIClient(s.sessions.APIURL(cfg), "")
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}
	return client, nil
}

// callbackError says what became of the wait when the API refuses an answer,
// because a holder decides from it whether to retry. The API error stays the
// cause, so the exit code still reflects its status.
func callbackError(action string, err error) error {
	var message string
	switch api.Code(err) {
	case "callback_not_found":
		message = "no callback has this token; its execution may be past its retention"
	case "callback_completed":
		message = "the callback already has an outcome; a token takes one answer, " +
			"so a retry of an answer that went through ends here too"
	case "callback_expired":
		message = "the callback timed out; nothing will take an answer, so do not retry"
	case "execution_ended":
		message = "the callback's execution has ended; nothing will take an answer, so do not retry"
	default:
		return fmt.Errorf("failed to %s the callback: %w", action, err)
	}
	return api.WithCause(errors.New(message), err)
}
