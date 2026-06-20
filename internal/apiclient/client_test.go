package apiclient

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

func TestGetProjectLogActivityUsesPostBody(t *testing.T) {
	projectID := ProjectId(uuid.MustParse("11111111-1111-4111-8111-111111111111"))
	resourceIDs := []string{"22222222-2222-4222-8222-222222222222"}
	levels := []apicommon.LiveLogLevel{apicommon.LiveLogLevelWarn}
	regions := []string{"us-east-1"}
	startTime := int64(1_700_000_000_000)
	endTime := int64(1_700_000_300_000)
	bucketCount := 24

	req, err := NewGetProjectLogActivityRequest("https://api.example.test", projectID, GetProjectLogActivityJSONRequestBody{
		ResourceType: apicommon.LogActivityRequestResourceTypeFunction,
		ResourceIds:  &resourceIDs,
		Levels:       &levels,
		Regions:      &regions,
		StartTime:    &startTime,
		EndTime:      &endTime,
		BucketCount:  &bucketCount,
	})
	require.NoError(t, err)

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/projects/11111111-1111-4111-8111-111111111111/logs/activity", req.URL.Path)
	assert.Empty(t, req.URL.RawQuery)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
	assert.Equal(t, "function", body["resource_type"])
	assert.Equal(t, []any{"22222222-2222-4222-8222-222222222222"}, body["resource_ids"])
	assert.Equal(t, []any{"warn"}, body["levels"])
	assert.Equal(t, []any{"us-east-1"}, body["regions"])
	assert.InEpsilon(t, startTime, body["start_time"], 0)
	assert.InEpsilon(t, endTime, body["end_time"], 0)
	assert.InEpsilon(t, bucketCount, body["bucket_count"], 0)
}
