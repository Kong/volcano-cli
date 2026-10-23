package deploycommit

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1; this matches it, it does not secure anything
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// File is one file an upload holds, recorded from the bytes packaging read,
// so a later change to the file on disk cannot change what is compared.
type File struct {
	// Path is the file packaging read.
	Path string
	// Executable is the executable bit the archive keeps for the file, nil
	// when the archive keeps none.
	Executable *bool

	// raw is the blob ID of the bytes as read; lf is it with CRLF line
	// endings turned to LF, empty when there are none; lfs is the Git LFS
	// pointer to the bytes.
	raw, lf, lfs string
}

// NewFile records an uploaded file from the bytes packaging read.
func NewFile(path string, content []byte, executable *bool) File {
	file := File{Path: path, Executable: executable, raw: blobID(content), lfs: blobID(lfsPointer(content))}
	if bytes.Contains(content, []byte("\r\n")) {
		file.lf = blobID(bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n")))
	}
	return file
}

// blobID is the ID git gives a blob with this content.
func blobID(content []byte) string {
	hash := sha1.New() //nolint:gosec // see the import
	fmt.Fprintf(hash, "blob %d\x00", len(content))
	hash.Write(content)
	return hex.EncodeToString(hash.Sum(nil))
}

// lfsPointer is the pointer Git LFS commits in place of content, as its
// specification fixes it. A commit holding this pointer holds the content.
func lfsPointer(content []byte) []byte {
	return fmt.Appendf(nil, "version https://git-lfs.github.com/spec/v1\noid sha256:%x\nsize %d\n", sha256.Sum256(content), len(content))
}
