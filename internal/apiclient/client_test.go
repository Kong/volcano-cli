package apiclient

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectLogActivityUsesPostBody(t *testing.T) {
	projectID := ProjectId(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	resourceIDs := []string{"22222222-2222-4222-8222-222222222222"}
	levels := []string{"warn"}
	regions := []string{"us-east-1"}
	startTime := int64(1_700_000_000_000)
	endTime := int64(1_700_000_300_000)
	bucketCount := 24
	requestBody, err := json.Marshal(map[string]any{
		"resource": map[string]any{
			"type": "function",
			"ids":  resourceIDs,
		},
		"levels":       levels,
		"regions":      regions,
		"start_time":   startTime,
		"end_time":     endTime,
		"bucket_count": bucketCount,
	})
	require.NoError(t, err)

	req, err := NewGetProjectLogActivityRequestWithBody("https://api.example.test", projectID, "application/json", bytes.NewReader(requestBody))
	require.NoError(t, err)

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/projects/11111111-1111-4111-8111-111111111111/logs/activity", req.URL.Path)
	assert.Empty(t, req.URL.RawQuery)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	var decodedBody map[string]any
	require.NoError(t, json.NewDecoder(req.Body).Decode(&decodedBody))
	resource, ok := decodedBody["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", resource["type"])
	assert.Equal(t, []any{resourceIDs[0]}, resource["ids"])
	assert.Equal(t, []any{levels[0]}, decodedBody["levels"])
	assert.Equal(t, []any{regions[0]}, decodedBody["regions"])
	assert.InEpsilon(t, startTime, decodedBody["start_time"], 0)
	assert.InEpsilon(t, endTime, decodedBody["end_time"], 0)
	assert.InEpsilon(t, bucketCount, decodedBody["bucket_count"], 0)
}
