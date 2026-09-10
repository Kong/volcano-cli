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

func TestGetProjectConfigYAMLResponseSizeBoundary(t *testing.T) {
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at limit", size: maxProjectConfigYAMLBytes},
		{name: "over limit", size: maxProjectConfigYAMLBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(bytes.Repeat([]byte("x"), test.size))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			body, err := client.GetProjectConfigYAML(context.Background(), uuid.New())
			if test.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "4194304-byte download limit")
				return
			}
			require.NoError(t, err)
			assert.Len(t, body, test.size)
		})
	}
}
