package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// DurableMessages renders one read of an execution's messages. nextCommand is
// the list command with its filters, so the hint for the rest keeps them.
func DurableMessages(w io.Writer, list *apiclient.DurableMessageList, nextCommand string) {
	if list == nil {
		list = &apiclient.DurableMessageList{}
	}

	on := theme.On(w)
	if len(list.Data) == 0 {
		fmt.Fprintln(w, "No messages found")
		return
	}

	DurableMessageTableHead(w)
	for i := range list.Data {
		DurableMessageTableRow(w, &list.Data[i])
	}
	summary(w, on, "Showing %d message(s)", len(list.Data))
	if list.HasMore {
		last := list.Data[len(list.Data)-1].Seq
		nextPage(w, on, fmt.Sprintf("%s --after %d", nextCommand, last))
	}
}

// DurableMessageTableHead writes the header DurableMessageTableRow lines up
// under.
func DurableMessageTableHead(w io.Writer) {
	tableHead(w, theme.On(w), true, 123, "%-6s  %-9s  %-24s  %-10s  %-12s  %-10s  %s",
		"Seq", "Direction", "Name", "Status", "Sender", "Sent", "Data")
}

// DurableMessageTableRow writes one message as a table row. The name and data
// come from a workflow or a sender, so they are printed without their control
// characters.
func DurableMessageTableRow(w io.Writer, message *apiclient.DurableMessage) {
	fmt.Fprintf(w, "%-6d  %-9s  %-24s  %s  %-12s  %-10s  %s\n",
		message.Seq,
		message.Direction,
		Truncate(theme.StripControl(message.Name), 24),
		statusCell(string(message.Status), 10, theme.On(w)),
		message.Sender.Type,
		FormatTimeAgo(message.SentAt),
		Truncate(compactMessageData(message.Data), 40),
	)
}

// DurableMessage renders one message.
func DurableMessage(w io.Writer, message *apiclient.DurableMessage) {
	if message == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "ID", "%s", message.Id.String())
	kv(w, on, "Seq", "%d", message.Seq)
	kv(w, on, "Direction", "%s", message.Direction)
	kv(w, on, "Name", "%s", theme.StripControl(message.Name))
	kv(w, on, "Status", "%s", theme.Status(string(message.Status), on))
	kv(w, on, "Sender", "%s", durableMessageSender(message.Sender))
	kv(w, on, "Sent", "%s", FormatTimestamp(message.SentAt))
	if message.DeliveredAt != nil {
		kv(w, on, "Delivered", "%s", FormatTimestamp(*message.DeliveredAt))
	}
	kv(w, on, "Data", "%s", workflowJSON(messageData(message.Data)))
}

// DurableMessageWaits renders the waits an execution has open.
func DurableMessageWaits(w io.Writer, executionID string, waits *apiclient.DurableMessageWaitList) {
	if waits == nil || len(waits.Data) == 0 {
		fmt.Fprintf(w, "Execution %s is not waiting for a message\n", executionID)
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 97, "%-36s  %-24s  %-9s  %-10s  %-10s", "ID", "Name", "Status", "Waiting", "Expires")
	for _, wait := range waits.Data {
		expires := "-"
		if wait.ExpiresAt != nil {
			expires = formatTimeUntil(*wait.ExpiresAt)
		}
		fmt.Fprintf(w, "%-36s  %-24s  %s  %-10s  %-10s\n",
			wait.Id.String(),
			Truncate(theme.StripControl(wait.Name), 24),
			statusCell(string(wait.Status), 9, on),
			FormatTimeAgo(wait.CreatedAt),
			expires,
		)
	}
}

func durableMessageSender(sender apiclient.DurableMessageSender) string {
	if sender.UserId != nil && *sender.UserId != "" {
		return fmt.Sprintf("%s (%s)", sender.Type, theme.StripControl(*sender.UserId))
	}
	return string(sender.Type)
}

// messageData is the message's data as a value to render. The API sends null
// for a message without data, and an absent field decodes to an empty raw
// value, which is the same message.
func messageData(data json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(data)) == 0 {
		return json.RawMessage("null")
	}
	return data
}

func compactMessageData(data json.RawMessage) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, messageData(data)); err != nil {
		return theme.StripControl(string(data))
	}
	return theme.StripControl(compact.String())
}
