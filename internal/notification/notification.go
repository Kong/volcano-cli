// Package notification performs the project notification settings workflows
// behind the volcano notifications subcommands.
package notification

import (
	"context"
	"fmt"
	"net/mail"
	"slices"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// When a failing scheduler is announced.
const (
	NotifyOnFirstFailure        = string(apiclient.SchedulerFailureNotifyOnFirstFailure)
	NotifyOnConsecutiveFailures = string(apiclient.SchedulerFailureNotifyOnConsecutiveFailures)
)

// Threshold bounds, as the API enforces them.
const (
	MinThreshold = 1
	MaxThreshold = 100
)

// MaxRecipients is the most email recipients the API accepts.
const MaxRecipients = 10

// NotifyOnValues lists the accepted --notify-on values.
func NotifyOnValues() []string {
	return []string{NotifyOnFirstFailure, NotifyOnConsecutiveFailures}
}

// Update is a settings change. A nil field keeps its current value.
type Update struct {
	EmailEnabled     *bool
	Recipients       *[]string
	WebhookURL       *string
	NotifyOn         *string
	Threshold        *int
	NotifyOnRecovery *bool
}

// Empty reports whether u changes nothing.
func (u Update) Empty() bool {
	return u == Update{}
}

// Validate refuses what the API would, before the request is sent.
func (u Update) Validate() error {
	if u.NotifyOn != nil && !slices.Contains(NotifyOnValues(), *u.NotifyOn) {
		return fmt.Errorf("invalid --notify-on %q: use %s", *u.NotifyOn, strings.Join(NotifyOnValues(), " or "))
	}
	if u.Threshold != nil && (*u.Threshold < MinThreshold || *u.Threshold > MaxThreshold) {
		return fmt.Errorf("--threshold must be between %d and %d", MinThreshold, MaxThreshold)
	}
	if u.Recipients != nil {
		return validateRecipients(recipientList(*u.Recipients))
	}
	return nil
}

// recipientList trims the addresses and drops blank and repeated ones, the way
// the API counts them, so a trailing comma in --recipients is not an address.
func recipientList(raw []string) []string {
	recipients := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, entry := range raw {
		address := strings.TrimSpace(entry)
		key := strings.ToLower(address)
		if address == "" || seen[key] {
			continue
		}
		seen[key] = true
		recipients = append(recipients, address)
	}
	return recipients
}

func validateRecipients(recipients []string) error {
	if len(recipients) > MaxRecipients {
		return fmt.Errorf("at most %d recipients can be set", MaxRecipients)
	}
	var invalid []string
	for _, address := range recipients {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Name != "" || parsed.Address != address {
			invalid = append(invalid, strconv.Quote(address))
		}
	}
	if len(invalid) > 0 {
		return fmt.Errorf("not an email address: %s", strings.Join(invalid, ", "))
	}
	return nil
}

func (u Update) request() apiclient.UpdateNotificationSettingsRequest {
	var body apiclient.UpdateNotificationSettingsRequest
	if u.EmailEnabled != nil || u.Recipients != nil {
		body.Email = &apiclient.UpdateNotificationEmail{Enabled: u.EmailEnabled}
		if u.Recipients != nil {
			list := recipientList(*u.Recipients)
			recipients := make([]openapi_types.Email, 0, len(list))
			for _, recipient := range list {
				recipients = append(recipients, openapi_types.Email(recipient))
			}
			body.Email.Recipients = &recipients
		}
	}
	if u.WebhookURL != nil {
		body.Webhook = &apiclient.UpdateNotificationWebhook{Url: u.WebhookURL}
	}
	if u.NotifyOn != nil || u.Threshold != nil || u.NotifyOnRecovery != nil {
		body.SchedulerFailures = &apiclient.UpdateSchedulerFailureAlertPolicy{
			Threshold:        u.Threshold,
			NotifyOnRecovery: u.NotifyOnRecovery,
		}
		if u.NotifyOn != nil {
			notifyOn := apiclient.SchedulerFailureNotifyOn(*u.NotifyOn)
			body.SchedulerFailures.NotifyOn = &notifyOn
		}
	}
	return body
}

// Service performs notification settings workflows against the current
// project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a notification settings service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Get returns the current project's notification settings.
func (s Service) Get(ctx context.Context) (*apiclient.NotificationSettings, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.GetNotificationSettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification settings: %w", err)
	}
	return settings, nil
}

// Update applies update to the current project's notification settings.
func (s Service) Update(ctx context.Context, update Update) (*apiclient.NotificationSettings, error) {
	if err := update.Validate(); err != nil {
		return nil, err
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.UpdateNotificationSettings(ctx, authenticated.ProjectID, update.request())
	if err != nil {
		return nil, fmt.Errorf("failed to update notification settings: %w", err)
	}
	return settings, nil
}

// RotateWebhookSecret replaces the current project's webhook signing secret.
func (s Service) RotateWebhookSecret(ctx context.Context) (*apiclient.NotificationSettings, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.RotateNotificationWebhookSecret(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to rotate the webhook signing secret: %w", err)
	}
	return settings, nil
}

// SendTest sends a test notification on every channel the current project
// has configured.
func (s Service) SendTest(ctx context.Context) (*apiclient.NotificationTestResult, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	result, err := authenticated.API.SendTestNotification(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to send a test notification: %w", err)
	}
	return result, nil
}
