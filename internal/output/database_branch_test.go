package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func outputBranch(t *testing.T, expiresAt time.Time) apiclient.DatabaseBranch {
	t.Helper()
	branchID, err := uuid.Parse("55555555-5555-4555-8555-555555555555")
	require.NoError(t, err)
	return apiclient.DatabaseBranch{
		Id:         branchID,
		Name:       "feature_x",
		Status:     apiclient.DatabaseBranchStatusActive,
		TtlSeconds: int64((48 * time.Hour).Seconds()),
		ExpiresAt:  expiresAt,
	}
}

func TestDatabaseBranchCountsDownARunningLifetime(t *testing.T) {
	branch := outputBranch(t, time.Now().Add(47*time.Hour))

	var detail bytes.Buffer
	DatabaseBranch(&detail, &branch, false)
	assert.Contains(t, detail.String(), "(in 1d)")

	var list bytes.Buffer
	DatabaseBranches(&list, []apiclient.DatabaseBranch{branch}, "app")
	assert.Contains(t, list.String(), "1d")
}

// An expired lifetime is not something that happens "in" a while, so the detail
// view drops the preposition the running-down case needs.
func TestDatabaseBranchLabelsAnExpiredLifetimeWithoutAPreposition(t *testing.T) {
	branch := outputBranch(t, time.Now().Add(-time.Minute))

	var detail bytes.Buffer
	DatabaseBranch(&detail, &branch, false)
	assert.Contains(t, detail.String(), "(expired)")
	assert.NotContains(t, detail.String(), "in expired")

	var list bytes.Buffer
	DatabaseBranches(&list, []apiclient.DatabaseBranch{branch}, "app")
	assert.Contains(t, list.String(), "expired")
	assert.NotContains(t, list.String(), "in expired")
}

// expires_at is required, so an absent one means a malformed response rather
// than a branch whose deadline has passed.
func TestDatabaseBranchWithoutADeadlineReadsAsAbsent(t *testing.T) {
	branch := outputBranch(t, time.Time{})

	var detail bytes.Buffer
	DatabaseBranch(&detail, &branch, false)
	assert.Contains(t, detail.String(), "Expires: -\n")
	assert.NotContains(t, detail.String(), "expired")

	var list bytes.Buffer
	DatabaseBranches(&list, []apiclient.DatabaseBranch{branch}, "app")
	assert.NotContains(t, list.String(), "expired")
}

// The column is one cell wide, so a lifetime is truncated to its largest whole
// unit: a branch with less than a minute left reads 0m, and one with less than
// two days reads 1d.
func TestFormatBranchDurationTruncatesToTheLargestUnit(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "sub-minute", d: 30 * time.Second, want: "0m"},
		{name: "minutes", d: 90 * time.Minute, want: "1h"},
		{name: "hours", d: 23 * time.Hour, want: "23h"},
		{name: "just under two days", d: 47 * time.Hour, want: "1d"},
		{name: "default lifetime", d: 7 * 24 * time.Hour, want: "7d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatBranchDuration(tc.d))
			assert.Equal(t, tc.want, formatBranchTTL(int64(tc.d.Seconds())))
		})
	}
}

func TestDatabaseBranchExplainsAWithheldConnectionString(t *testing.T) {
	branch := outputBranch(t, time.Now().Add(24*time.Hour))
	branch.Status = apiclient.DatabaseBranchStatusProvisioning

	var out bytes.Buffer
	DatabaseBranch(&out, &branch, true)
	assert.Contains(t, out.String(), "Connection string: - (issued once the branch is active)")
}

func TestDatabaseBranchRotatedConnectionStringReportsAnAbsentString(t *testing.T) {
	branch := outputBranch(t, time.Now().Add(24*time.Hour))

	var missing bytes.Buffer
	DatabaseBranchRotatedConnectionString(&missing, &branch, "app", "volcano cloud")
	assert.Contains(t, missing.String(), "Warning: the API returned no connection string for branch 'feature_x'")
	assert.Contains(t, missing.String(), "volcano cloud databases branches get app feature_x --show-connection-string")

	connectionString := "postgresql://branch:s3cr3t@host/db"
	branch.ConnectionString = &connectionString

	var rotated bytes.Buffer
	DatabaseBranchRotatedConnectionString(&rotated, &branch, "app", "volcano cloud")
	assert.Contains(t, rotated.String(), connectionString)
	assert.NotContains(t, rotated.String(), "Warning:")
}
