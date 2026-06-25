package apiclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectLogActivityUsesPostBody(t *testing.T) {
	projectID := ProjectId(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	startTime := int64(1_700_000_000_000)
	endTime := int64(1_700_000_300_000)
	bucketCount := 24
	requestBody := `{"resource":{"type":"function","ids":["22222222-2222-4222-8222-222222222222"]},"levels":["warn"],"regions":["us-east-1"],"start_time":1700000000000,"end_time":1700000300000,"bucket_count":24}`

	req, err := NewGetProjectLogActivityRequestWithBody("https://api.example.test", projectID, "application/json", strings.NewReader(requestBody))
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
	assert.Equal(t, []any{"22222222-2222-4222-8222-222222222222"}, resource["ids"])
	assert.Equal(t, []any{"warn"}, decodedBody["levels"])
	assert.Equal(t, []any{"us-east-1"}, decodedBody["regions"])
	assert.InEpsilon(t, startTime, decodedBody["start_time"], 0)
	assert.InEpsilon(t, endTime, decodedBody["end_time"], 0)
	assert.InEpsilon(t, bucketCount, decodedBody["bucket_count"], 0)
}
