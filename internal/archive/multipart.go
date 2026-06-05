package archive

import (
	"fmt"
	"mime/multipart"
)

// WriteArchivePart attaches data as fieldName in writer, using baseName+".tar.gz"
// as the filename. baseName should be the resource name (e.g. function or
// frontend name); the helper appends the canonical archive suffix.
func WriteArchivePart(writer *multipart.Writer, fieldName, baseName string, data []byte) error {
	part, err := writer.CreateFormFile(fieldName, baseName+".tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create %s form file: %w", fieldName, err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("failed to write %s archive: %w", fieldName, err)
	}
	return nil
}
