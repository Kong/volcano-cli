package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

const (
	// PartSize is the most file data one file part carries.
	PartSize = 8 << 20
	// MaxPartedBytes is the largest file sent in parts.
	MaxPartedBytes = 1 << 30
)

// LargeFile is a file too large for one archive call, sent on its own with
// file parts.
type LargeFile struct {
	// Name is slash-separated and relative to the uploaded directory.
	Name string
	// Path is the file's local path.
	Path   string
	Size   int64
	upload *Upload
	entry  uploadEntry
}

// FilePart is one file part: Data is the file's bytes at Offset, and Size
// and SHA256 describe the whole file.
type FilePart struct {
	Offset int64
	Size   int64
	SHA256 string
	Data   []byte
}

// LargeFiles returns the files the archives leave out, in walk order.
func (u *Upload) LargeFiles() []LargeFile {
	files := make([]LargeFile, 0, len(u.large))
	for _, e := range u.large {
		files = append(files, LargeFile{Name: e.name, Path: u.localPath(e.name), Size: e.size, upload: u, entry: e})
	}
	return files
}

// Parts reads the file once to hash it, then passes its parts to send in
// order, stopping at the first error. A part's Data is reused once send
// returns.
func (f LargeFile) Parts(send func(FilePart) error) error {
	file, err := f.upload.open(f.entry)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.CopyN(hash, file, f.Size); err != nil {
		return f.readError(err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	data := make([]byte, min(PartSize, f.Size))
	for offset := int64(0); offset < f.Size; {
		n := min(PartSize, f.Size-offset)
		if _, err := file.ReadAt(data[:n], offset); err != nil {
			return f.readError(err)
		}
		if err := send(FilePart{Offset: offset, Size: f.Size, SHA256: sum, Data: data[:n]}); err != nil {
			return err
		}
		offset += n
	}
	return nil
}

// readError reads a short file as one that changed after the walk.
func (f LargeFile) readError(err error) error {
	if errors.Is(err, io.EOF) {
		return f.upload.changed(f.Name)
	}
	return err
}
