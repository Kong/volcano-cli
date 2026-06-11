package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// StorageBuckets renders one bucket list.
func StorageBuckets(w io.Writer, buckets []apiclient.StorageBucket) {
	if len(buckets) == 0 {
		fmt.Fprintln(w, "No storage buckets")
		return
	}

	fmt.Fprintf(w, "\n%-32s  %-38s  %-15s  %-15s\n", "Name", "ID", "Size limit", "Created")
	fmt.Fprintln(w, strings.Repeat("-", 110))
	for _, bucket := range buckets {
		fmt.Fprintf(w, "%-32s  %-38s  %-15s  %-15s\n",
			Truncate(bucket.Name, 32),
			bucket.Id.String(),
			formatBucketSizeLimit(bucket.FileSizeLimit),
			FormatTimeAgo(timeOrZero(bucket.CreatedAt)),
		)
	}
	fmt.Fprintf(w, "\nTotal: %d bucket(s)\n", len(buckets))
}

// StorageBucket renders one bucket detail view.
func StorageBucket(w io.Writer, bucket *apiclient.StorageBucket) {
	if bucket == nil {
		return
	}
	fmt.Fprintf(w, "Name: %s\n", bucket.Name)
	fmt.Fprintf(w, "ID: %s\n", bucket.Id.String())
	fmt.Fprintf(w, "File size limit: %s\n", formatBucketSizeLimit(bucket.FileSizeLimit))
	if bucket.AllowedMimeTypes != nil && len(*bucket.AllowedMimeTypes) > 0 {
		fmt.Fprintf(w, "Allowed MIME types: %s\n", strings.Join(*bucket.AllowedMimeTypes, ", "))
	} else {
		fmt.Fprintln(w, "Allowed MIME types: any")
	}
	if bucket.LastInvokedAt != nil {
		fmt.Fprintf(w, "Last invoked: %s\n", FormatTimestamp(*bucket.LastInvokedAt))
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(timeOrZero(bucket.CreatedAt)))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(timeOrZero(bucket.UpdatedAt)))
}

// StoragePolicies renders one policy list.
func StoragePolicies(w io.Writer, bucketName string, policies []apiclient.StoragePolicy) {
	if len(policies) == 0 {
		fmt.Fprintf(w, "No policies on bucket '%s'\n", bucketName)
		return
	}

	fmt.Fprintf(w, "\n%-24s  %-10s  %-38s  %-15s\n", "Name", "Operation", "ID", "Created")
	fmt.Fprintln(w, strings.Repeat("-", 95))
	for _, policy := range policies {
		fmt.Fprintf(w, "%-24s  %-10s  %-38s  %-15s\n",
			Truncate(policy.Name, 24),
			strings.TrimSpace(string(policy.Operation)),
			policy.Id.String(),
			FormatTimeAgo(timeOrZero(policy.CreatedAt)),
		)
	}
	fmt.Fprintf(w, "\nTotal: %d policy(ies) on bucket '%s'\n", len(policies), bucketName)
}

// StoragePolicy renders one policy detail view.
func StoragePolicy(w io.Writer, bucketName string, policy *apiclient.StoragePolicy) {
	if policy == nil {
		return
	}
	fmt.Fprintf(w, "Bucket: %s\n", bucketName)
	fmt.Fprintf(w, "Name: %s\n", policy.Name)
	fmt.Fprintf(w, "ID: %s\n", policy.Id.String())
	fmt.Fprintf(w, "Operation: %s\n", strings.TrimSpace(string(policy.Operation)))
	fmt.Fprintln(w, "Definition:")
	fmt.Fprintf(w, "  %s\n", policy.Definition)
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(timeOrZero(policy.CreatedAt)))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(timeOrZero(policy.UpdatedAt)))
}

// StorageObjects renders one cursor page of objects.
func StorageObjects(w io.Writer, bucketName string, page *apiclient.StorageListResponse, commandPrefix ...string) {
	objects := storageObjects(page)
	if len(objects) == 0 {
		fmt.Fprintf(w, "No objects in bucket '%s'\n", bucketName)
		return
	}

	fmt.Fprintf(w, "\n%-48s  %12s  %-24s  %-7s  %-15s\n", "Path", "Size", "MIME type", "Public", "Updated")
	fmt.Fprintln(w, strings.Repeat("-", 117))
	for _, object := range objects {
		fmt.Fprintf(w, "%-48s  %12s  %-24s  %-7s  %-15s\n",
			Truncate(object.Name, 48),
			formatByteSize(object.Size),
			Truncate(object.MimeType, 24),
			formatBool(object.IsPublic),
			FormatTimeAgo(timeOrZero(object.UpdatedAt)),
		)
	}
	fmt.Fprintf(w, "\nTotal: %d object(s) on this page\n", len(objects))
	if nextCursor := storageNextCursor(page); nextCursor != "" {
		fmt.Fprintf(w, "Next page: %s storage object list %s --cursor %s\n", commandPathPrefix(commandPrefix), bucketName, nextCursor)
	}
}

// StorageObject renders one object detail view.
func StorageObject(w io.Writer, bucketName string, object *apiclient.StorageObject) {
	if object == nil {
		return
	}
	fmt.Fprintf(w, "Bucket: %s\n", bucketName)
	fmt.Fprintf(w, "Path: %s\n", object.Name)
	fmt.Fprintf(w, "ID: %s\n", object.Id.String())
	fmt.Fprintf(w, "Size: %s\n", formatByteSize(object.Size))
	fmt.Fprintf(w, "MIME type: %s\n", object.MimeType)
	fmt.Fprintf(w, "Public: %s\n", formatBool(object.IsPublic))
	if object.PublicUrl != nil && *object.PublicUrl != "" {
		fmt.Fprintf(w, "Public URL: %s\n", *object.PublicUrl)
	}
	if object.Etag != nil && *object.Etag != "" {
		fmt.Fprintf(w, "ETag: %s\n", *object.Etag)
	}
	fmt.Fprintf(w, "Created: %s\n", FormatTimestamp(timeOrZero(object.CreatedAt)))
	fmt.Fprintf(w, "Updated: %s\n", FormatTimestamp(timeOrZero(object.UpdatedAt)))
}

// StorageStats renders aggregate project storage usage.
func StorageStats(w io.Writer, stats *apiclient.StorageStats) {
	if stats == nil {
		return
	}
	fmt.Fprintf(w, "Buckets: %d\n", stats.BucketCount)
	fmt.Fprintf(w, "Objects: %d\n", stats.ObjectCount)
	fmt.Fprintf(w, "Total size: %s\n", formatByteSize(stats.TotalSize))
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
