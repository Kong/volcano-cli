package deploycommit

import (
	"bytes"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1; this matches it, it does not secure anything
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// File is one file an upload holds, recorded from the bytes packaging wrote,
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
	// binary is whether git's text=auto would leave the bytes as they are.
	binary bool
}

// Copy writes src, which holds size bytes, to dst and records the bytes as the
// file at path, without holding them in memory. When they have CRLF line
// endings it reads src again from the start to record them with LF; if they
// changed meanwhile, only what was written is recorded.
func Copy(dst io.Writer, src io.ReadSeeker, size int64, path string, executable *bool) (File, error) {
	raw, sum := newBlobHash(size), sha256.New()
	var stats textStats
	written, err := io.Copy(io.MultiWriter(dst, raw, sum, &stats), src)
	if err != nil {
		return File{}, err
	}
	if written != size {
		return File{}, fmt.Errorf("%s changed size while it was read", path)
	}
	file := File{
		Path:       path,
		Executable: executable,
		raw:        hex.EncodeToString(raw.Sum(nil)),
		lfs:        blobID(fmt.Appendf(nil, lfsPointerFormat, sum.Sum(nil), size)),
		binary:     stats.binary(),
	}
	if stats.crlf > 0 {
		file.lf = lfBlobID(src, size, stats.crlf, file.raw)
	}
	return file, nil
}

// lfBlobID reads the size bytes src holds again from the start and returns
// the blob ID of them with their crlf CRLF line endings turned to LF. It
// returns "" when they can't be read or no longer have the blob ID raw.
func lfBlobID(src io.ReadSeeker, size, crlf int64, raw string) string {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	again, lf := newBlobHash(size), newBlobHash(size-crlf)
	normalize := &lfWriter{w: lf}
	if _, err := io.CopyN(io.MultiWriter(again, normalize), src, size); err != nil {
		return ""
	}
	normalize.flush()
	if hex.EncodeToString(again.Sum(nil)) != raw {
		return ""
	}
	return hex.EncodeToString(lf.Sum(nil))
}

// NewFile records an uploaded file from the bytes packaging read.
func NewFile(path string, content []byte, executable *bool) File {
	file, _ := Copy(io.Discard, bytes.NewReader(content), int64(len(content)), path, executable)
	return file
}

// newBlobHash starts the hash git names a blob of size bytes by.
func newBlobHash(size int64) hash.Hash {
	h := sha1.New() //nolint:gosec // see the import
	fmt.Fprintf(h, "blob %d\x00", size)
	return h
}

// blobID is the ID git gives a blob with this content.
func blobID(content []byte) string {
	h := newBlobHash(int64(len(content)))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// lfsPointerFormat is the pointer Git LFS commits in place of content, as its
// specification fixes it, from the content's SHA-256 and size.
const lfsPointerFormat = "version https://git-lfs.github.com/spec/v1\noid sha256:%x\nsize %d\n"

// lfsPointer is the pointer Git LFS commits in place of content.
func lfsPointer(content []byte) []byte {
	return fmt.Appendf(nil, lfsPointerFormat, sha256.Sum256(content), len(content))
}

// textStats counts what git's text=auto looks at to decide whether bytes are
// text, as git's convert.c gathers it.
type textStats struct {
	crlf, loneCR, nul, printable, nonPrintable int64
	// cr is whether the last byte seen was a CR not yet paired; last is the
	// last byte seen.
	cr   bool
	last byte
	seen bool
}

func (s *textStats) Write(p []byte) (int, error) {
	for _, c := range p {
		if s.cr {
			s.cr = false
			if c == '\n' {
				s.crlf++
				s.last = c
				continue
			}
			s.loneCR++
		}
		switch {
		case c == '\r':
			s.cr = true
		case c == '\n':
		case c == 127:
			s.nonPrintable++
		case c < 32:
			switch c {
			case '\b', '\t', '\033', '\f':
				s.printable++
			case 0:
				s.nul++
				s.nonPrintable++
			default:
				s.nonPrintable++
			}
		default:
			s.printable++
		}
		s.last, s.seen = c, true
	}
	return len(p), nil
}

// binary is git's convert_is_binary: a lone CR, a NUL, or more than one
// non-printable byte for each 128 printable ones. A final ^Z, an old
// end-of-file mark, does not count.
func (s *textStats) binary() bool {
	loneCR, nonPrintable := s.loneCR, s.nonPrintable
	if s.cr {
		loneCR++
	}
	if s.seen && s.last == '\032' {
		nonPrintable--
	}
	return loneCR > 0 || s.nul > 0 || s.printable>>7 < nonPrintable
}

// lfWriter writes the bytes it is given to w with each CRLF turned to LF.
type lfWriter struct {
	w  io.Writer
	cr bool
}

func (l *lfWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if l.cr {
			l.cr = false
			if p[0] != '\n' {
				if _, err := l.w.Write([]byte{'\r'}); err != nil {
					return 0, err
				}
			}
		}
		i := bytes.IndexByte(p, '\r')
		if i < 0 {
			if _, err := l.w.Write(p); err != nil {
				return 0, err
			}
			break
		}
		// Hold the CR back until the next byte says whether it ends a CRLF.
		if _, err := l.w.Write(p[:i]); err != nil {
			return 0, err
		}
		l.cr = true
		p = p[i+1:]
	}
	return n, nil
}

// flush writes a final CR held back in case an LF followed it.
func (l *lfWriter) flush() {
	if l.cr {
		l.cr = false
		_, _ = l.w.Write([]byte{'\r'})
	}
}
