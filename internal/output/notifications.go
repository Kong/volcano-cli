package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// NotificationSettings renders a project's notification settings, and the
// webhook signing secret when the response that generated it is the one being
// rendered.
func NotificationSettings(w io.Writer, settings *apiclient.NotificationSettings) {
	if settings == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Email", "%s", notificationEmail(settings.Email))
	webhook := settings.Webhook
	if url := stringPtrValue(webhook.Url); url != "" {
		kv(w, on, "Webhook", "%s", theme.StripControl(url))
		kv(w, on, "Signing secret", "%s", notificationSecretState(webhook.SecretConfigured))
	} else {
		kv(w, on, "Webhook", "not configured")
	}
	policy := settings.SchedulerFailures
	kv(w, on, "Alert on", "%s", schedulerAlertTrigger(policy))
	kv(w, on, "Recovery", "%s", recoveryAlert(policy.NotifyOnRecovery))

	if secret := stringPtrValue(webhook.Secret); secret != "" {
		fmt.Fprintf(w, "\n%s %s\n", theme.Dim("Signing secret:", on), secret)
		Warning(w, "Copy this secret now. It is shown once and cannot be retrieved again.")
	}
}

func notificationEmail(email apiclient.NotificationEmailSettings) string {
	if !email.Enabled {
		return "off"
	}
	// A server older than email.status leaves it empty.
	var delivery string
	if email.Status == apiclient.NotificationEmailStatusOwnerEmailUnverified {
		delivery = " (not delivering: owner email unverified)"
	}
	if len(email.Recipients) == 0 {
		return "on, to the project owner" + delivery
	}
	recipients := make([]string, 0, len(email.Recipients))
	for _, recipient := range email.Recipients {
		recipients = append(recipients, theme.StripControl(string(recipient)))
	}
	return "on, to " + strings.Join(recipients, ", ") + delivery
}

func notificationSecretState(configured bool) string {
	if configured {
		return "configured"
	}
	return "not set"
}

func schedulerAlertTrigger(policy apiclient.SchedulerFailureAlertPolicy) string {
	if policy.NotifyOn == apiclient.SchedulerFailureNotifyOnFirstFailure {
		return "a scheduler's first failed run"
	}
	if policy.Threshold == 1 {
		return "1 failed scheduled run"
	}
	return fmt.Sprintf("%d failed scheduled runs in a row", policy.Threshold)
}

func recoveryAlert(enabled bool) string {
	if enabled {
		return "notify when a failing scheduler succeeds again"
	}
	return "off"
}

// NotificationTestDelivered renders the test deliveries that arrived. The
// command reports the rest as its error.
func NotificationTestDelivered(w io.Writer, result *apiclient.NotificationTestResult) {
	if result == nil {
		return
	}
	for _, delivery := range result.Deliveries {
		if delivery.Delivered {
			Success(w, "Test %s delivered%s", delivery.Channel, NotificationDeliveryStatus(delivery))
		}
	}
}

// NotificationDeliveryStatus is the webhook response status of a delivery, as
// " (HTTP 200)", or "" when none arrived.
func NotificationDeliveryStatus(delivery apiclient.NotificationTestDelivery) string {
	if delivery.StatusCode == nil {
		return ""
	}
	return fmt.Sprintf(" (HTTP %d)", *delivery.StatusCode)
}
