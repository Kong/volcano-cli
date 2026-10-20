package output

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func outputDurableMessage(data string) *apiclient.DurableMessage {
	return &apiclient.DurableMessage{
		Id:          uuid.MustParse("55555555-5555-4555-8555-555555555555"),
		ExecutionId: uuid.MustParse("66666666-6666-4666-8666-666666666666"),
		Seq:         3,
		Direction:   apiclient.DurableMessageDirectionIn,
		Name:        "posted",
		Status:      apiclient.DurableMessageStatusDelivered,
		Sender:      apiclient.DurableMessageSender{Type: apiclient.DurableMessageSenderTypeProject},
		SentAt:      time.Now().Add(-time.Minute),
		Data:        json.RawMessage(data),
	}
}

func TestDurableMessageShowsDataAsWritten(t *testing.T) {
	var out bytes.Buffer
	DurableMessage(&out, outputDurableMessage(`{"url":"https://x.io/?a=1&b=<2>"}`))

	assert.Contains(t, out.String(), `"url": "https://x.io/?a=1&b=<2>"`)
}

func TestDurableMessageRowKeepsTruncatedDataValid(t *testing.T) {
	var out bytes.Buffer
	DurableMessageTableRow(&out, outputDurableMessage(`{"note":"日本語のメッセージ本文は表の列に収まらないほど長いので途中で切れます"}`))

	assert.True(t, utf8.ValidString(out.String()))
	assert.Contains(t, out.String(), "...")
}
