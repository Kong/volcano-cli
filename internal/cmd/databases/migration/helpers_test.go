package migration

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeTargetMigration(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   string
	}{
		{target: "001_create_users", want: "001_create_users"},
		{target: "001_create_users.sql", want: "001_create_users"},
		{target: filepath.Join("volcano", "migrations", "001_create_users.sql"), want: "001_create_users"},
		{target: filepath.Join("migrations", "001_create_users.SQL"), want: "001_create_users"},
	} {
		assert.Equal(t, tc.want, normalizeTargetMigration(tc.target))
	}
}
