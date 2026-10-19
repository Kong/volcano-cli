package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFrontendCustomDomains(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
frontends:
  - name: web
    custom_domains:
      - domain: www.example.com
        tls:
          mode: managed
      - domain: example.com
        tls:
          mode: managed
        redirect:
          to: www.example.com
      - domain: old.example.com
        tls:
          mode: byoc
          certificate_pem: ${CERT}
          private_key_pem: ${KEY}
        redirect:
          to: www.example.com
          status: 301
  - name: docs
    custom_domains: []
`), envMap(map[string]string{"CERT": "CERT", "KEY": "KEY"}))
	require.NoError(t, err)

	frontends := *manifest.Frontends
	require.NotNil(t, frontends[0].CustomDomains)
	domains := *frontends[0].CustomDomains
	require.Len(t, domains, 3)
	assert.Equal(t, "www.example.com", domains[0].Domain)
	assert.Equal(t, "managed", domains[0].TLS.Mode)
	assert.Nil(t, domains[0].Redirect)
	require.NotNil(t, domains[1].Redirect)
	assert.Equal(t, "www.example.com", domains[1].Redirect.To)
	assert.Nil(t, domains[1].Redirect.Status)
	assert.Equal(t, "CERT", domains[2].TLS.CertificatePEM)
	assert.Equal(t, 301, *domains[2].Redirect.Status)
	require.NotNil(t, frontends[1].CustomDomains)
	assert.Empty(t, *frontends[1].CustomDomains)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	var decoded struct {
		Frontends []struct {
			CustomDomains []map[string]any `json:"custom_domains"`
		} `json:"frontends"`
	}
	require.NoError(t, json.Unmarshal(body, &decoded))
	uploaded := decoded.Frontends[0].CustomDomains
	require.Len(t, uploaded, 3)
	assert.Equal(t, map[string]any{"mode": "managed"}, uploaded[0]["tls"])
	assert.NotContains(t, uploaded[0], "redirect")
	assert.Equal(t, map[string]any{"to": "www.example.com"}, uploaded[1]["redirect"])
	assert.Equal(t, map[string]any{"to": "www.example.com", "status": float64(301)}, uploaded[2]["redirect"])
	require.NotNil(t, decoded.Frontends[1].CustomDomains)
	assert.Empty(t, decoded.Frontends[1].CustomDomains)
}

func TestParseFrontendCustomDomainsRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`version: 1
frontends:
  - name: web
    custom_domains:
      - domain: example.com
        redirect:
          target: www.example.com
`), noEnv)
	require.ErrorContains(t, err, "field target not found")
}
