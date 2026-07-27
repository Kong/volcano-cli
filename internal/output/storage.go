package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// StorageBuckets renders one bucket list.
func StorageBuckets(w io.Writer, buckets []apiclient.StorageBucket) {
	if len(buckets) == 0 {
		fmt.Fprintln(w, "No storage buckets")
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 110, "%-32s  %-38s  %-15s  %-15s", "Name", "ID", "Size limit", "Created")
	for _, bucket := range buckets {
		fmt.Fprintf(w, "%-32s  %-38s  %-15s  %-15s\n",
			Truncate(bucket.Name, 32),
			bucket.Id.String(),
			formatBucketSizeLimit(bucket.FileSizeLimit),
			FormatTimeAgo(timeOrZero(bucket.CreatedAt)),
		)
	}
	summary(w, on, "Total: %d bucket(s)", len(buckets))
}

// StorageBucket renders one bucket detail view.
func StorageBucket(w io.Writer, bucket *apiclient.StorageBucket) {
	if bucket == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Name", "%s", bucket.Name)
	kv(w, on, "ID", "%s", bucket.Id.String())
	kv(w, on, "File size limit", "%s", formatBucketSizeLimit(bucket.FileSizeLimit))
	if bucket.AllowedMimeTypes != nil && len(*bucket.AllowedMimeTypes) > 0 {
		kv(w, on, "Allowed MIME types", "%s", strings.Join(*bucket.AllowedMimeTypes, ", "))
	} else {
		kv(w, on, "Allowed MIME types", "%s", "any")
	}
	if bucket.LastInvokedAt != nil {
		kv(w, on, "Last invoked", "%s", FormatTimestamp(*bucket.LastInvokedAt))
	}
	kv(w, on, "Created", "%s", FormatTimestamp(timeOrZero(bucket.CreatedAt)))
	kv(w, on, "Updated", "%s", FormatTimestamp(timeOrZero(bucket.UpdatedAt)))
}

// StoragePolicies renders one policy list.
func StoragePolicies(w io.Writer, bucketName string, policies []apiclient.StoragePolicy) {
	if len(policies) == 0 {
		fmt.Fprintf(w, "No policies on bucket '%s'\n", bucketName)
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 95, "%-24s  %-10s  %-38s  %-15s", "Name", "Operation", "ID", "Created")
	for _, policy := range policies {
		fmt.Fprintf(w, "%-24s  %-10s  %-38s  %-15s\n",
			Truncate(policy.Name, 24),
			strings.TrimSpace(string(policy.Operation)),
			policy.Id.String(),
			FormatTimeAgo(timeOrZero(policy.CreatedAt)),
		)
	}
	summary(w, on, "Total: %d policy(ies) on bucket '%s'", len(policies), bucketName)
}

// StoragePolicy renders one policy detail view.
func StoragePolicy(w io.Writer, bucketName string, policy *apiclient.StoragePolicy) {
	if policy == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Bucket", "%s", bucketName)
	kv(w, on, "Name", "%s", policy.Name)
	kv(w, on, "ID", "%s", policy.Id.String())
	kv(w, on, "Operation", "%s", strings.TrimSpace(string(policy.Operation)))
	fmt.Fprintln(w, theme.Dim("Definition:", on))
	fmt.Fprintf(w, "  %s\n", policy.Definition)
	kv(w, on, "Created", "%s", FormatTimestamp(timeOrZero(policy.CreatedAt)))
	kv(w, on, "Updated", "%s", FormatTimestamp(timeOrZero(policy.UpdatedAt)))
}

// StorageObjects renders one cursor page of objects.
func StorageObjects(w io.Writer, bucketName string, page *apiclient.StorageListResponse, commandPrefix ...string) {
	objects := storageObjects(page)
	if len(objects) == 0 {
		fmt.Fprintf(w, "No objects in bucket '%s'\n", bucketName)
		return
	}

	on := theme.On(w)
	tableHead(w, on, true, 117, "%-48s  %12s  %-24s  %-7s  %-15s", "Path", "Size", "MIME type", "Public", "Updated")
	for _, object := range objects {
		fmt.Fprintf(w, "%-48s  %12s  %-24s  %s  %-15s\n",
			Truncate(object.Name, 48),
			formatByteSize(object.Size),
			Truncate(object.MimeType, 24),
			statusCell(formatBool(object.IsPublic), 7, on),
			FormatTimeAgo(timeOrZero(object.UpdatedAt)),
		)
	}
	summary(w, on, "Total: %d object(s) on this page", len(objects))
	if nextCursor := storageNextCursor(page); nextCursor != "" {
		fmt.Fprintf(w, "%s%s\n", theme.Dim("Next page: ", on), theme.Command(fmt.Sprintf("%s storage object list %s --cursor %s", commandPathPrefix(commandPrefix), bucketName, nextCursor), on))
	}
}

// StorageObject renders one object detail view.
func StorageObject(w io.Writer, bucketName string, object *apiclient.StorageObject) {
	if object == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Bucket", "%s", bucketName)
	kv(w, on, "Path", "%s", object.Name)
	kv(w, on, "ID", "%s", object.Id.String())
	kv(w, on, "Size", "%s", formatByteSize(object.Size))
	kv(w, on, "MIME type", "%s", object.MimeType)
	kv(w, on, "Public", "%s", theme.Status(formatBool(object.IsPublic), on))
	if object.PublicUrl != nil && *object.PublicUrl != "" {
		kv(w, on, "Public URL", "%s", *object.PublicUrl)
	}
	if object.Etag != nil && *object.Etag != "" {
		kv(w, on, "ETag", "%s", *object.Etag)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(timeOrZero(object.CreatedAt)))
	kv(w, on, "Updated", "%s", FormatTimestamp(timeOrZero(object.UpdatedAt)))
}

// StorageStats renders aggregate project storage usage.
func StorageStats(w io.Writer, stats *apiclient.StorageStats) {
	if stats == nil {
		return
	}
	on := theme.On(w)
	kv(w, on, "Buckets", "%d", stats.BucketCount)
	kv(w, on, "Objects", "%d", stats.ObjectCount)
	kv(w, on, "Total size", "%s", formatByteSize(stats.TotalSize))
}

func storageObjects(page *apiclient.StorageListResponse) []apiclient.StorageObject {
	if page == nil || page.Objects == nil {
		return nil
	}
	return *page.Objects
}

func storageNextCursor(page *apiclient.StorageListResponse) string {
	if page == nil || page.NextCursor == nil {
		return ""
	}
	return strings.TrimSpace(*page.NextCursor)
}

func formatBucketSizeLimit(limit *int64) string {
	if limit == nil {
		return "unlimited"
	}
	return formatByteSize(*limit)
}

func formatByteSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), suffix)
}

func formatBool(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
