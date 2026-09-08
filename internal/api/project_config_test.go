package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectConfigYAMLRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxProjectConfigYAMLBytes+1))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.GetProjectConfigYAML(context.Background(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "4194304-byte download limit")
}
