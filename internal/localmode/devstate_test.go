package localmode

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevStateSaveDeletePermissionsAndShape(t *testing.T) {
	setLocalDevTestHome(t)
	info := localModeInfo("http://localhost:8000")

	require.NoError(t, saveDevState(info))
	statePath, err := DevStatePath()
	require.NoError(t, err)

	stat, err := os.Stat(statePath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm())

	var state DevState
	data, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, info.JWTSecret, state.JWTSecret)
	assert.Equal(t, info.EncryptionKey, state.EncryptionKey)
	assert.Equal(t, info.AnonKeySecret, state.AnonKeySecret)
	assert.Equal(t, info.ServiceKeySecret, state.ServiceKeySecret)
	assert.Equal(t, info.AuthUserID, state.AuthUserID)
	assert.Equal(t, info.UserToken, state.UserToken)
	assert.Equal(t, info.DatabaseURL, state.DatabaseURL)
	assert.Equal(t, info.RedisURL, state.RedisURL)

	require.NoError(t, deleteDevState())
	_, err = os.Stat(statePath)
	require.True(t, os.IsNotExist(err), "expected dev state to be deleted, got %v", err)
}
