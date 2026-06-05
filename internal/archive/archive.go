// Package archive contains shared helpers for working with tar.gz archives
// produced by the CLI: byte-size formatting and multipart upload framing.
package archive

import (
	"fmt"
	"strings"
)

// FormatSize renders a byte count as a compact "B/KB/MB/GB/TB" string.
func FormatSize(sizeBytes int64) string {
	if sizeBytes <= 0 {
		return "0B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	size := float64(sizeBytes)
	unitIdx := 0
	for size >= 1024 && unitIdx < len(units)-1 {
		size /= 1024
		unitIdx++
	}
	if unitIdx == 0 {
		return fmt.Sprintf("%dB", sizeBytes)
	}
	formatted := fmt.Sprintf("%.1f", size)
	formatted = strings.TrimSuffix(formatted, ".0")
	return formatted + units[unitIdx]
}
