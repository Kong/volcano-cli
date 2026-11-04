package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
)

// UploadLimits bound one file-archive call.
type UploadLimits struct {
	// Body bounds the gzip-compressed archive.
	Body int64
	// Entries bounds files and directories, counting the directories that
	// entry names imply.
	Entries int
	// Expanded bounds the uncompressed tar stream, headers and padding
	// included.
	Expanded int64
}

// ArchiveLimits are the API's limits for one file-archive call.
var ArchiveLimits = UploadLimits{Body: 32 << 20, Entries: 10_000, Expanded: 256 << 20}

// Upload is a local directory packed into as many archives as the
// archive limits need, in walk order, and the files too large for one
// archive, which are sent on their own in parts.
type Upload struct {
	directory string
	root      *os.Root
	limits    UploadLimits
	entries   []uploadEntry
	large     []uploadEntry
}

type uploadEntry struct {
	// name is slash-separated and relative to the uploaded directory.
	name string
	dir  bool
	mode fs.FileMode
	size int64
	// header is the size of its tar header, extended headers included.
	header int64
}

const (
	tarBlock = 512
	// tarTrailer is the two zero blocks that end every archive.
	tarTrailer = 2 * tarBlock
)

// gzipHeader is a gzip member header with no name or timestamp.
var gzipHeader = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff}

// OpenUpload walks directory and refuses, before anything is sent, a symbolic
// link, any other entry that is not a regular file or directory, and a file
// too large even to send in parts. Symbolic links are never followed. The
// caller must Close the result.
func OpenUpload(directory string, limits UploadLimits) (*Upload, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	u := &Upload{directory: directory, root: root, limits: limits}
	if err := u.walk(); err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := u.setAsideLargeFiles(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return u, nil
}

// Close releases the directory.
func (u *Upload) Close() error {
	return u.root.Close()
}

func (u *Upload) walk() error {
	return fs.WalkDir(u.root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		kind := entry.Type()
		switch {
		case kind&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a symbolic link; uploads copy only regular files and directories", u.localPath(name))
		case !kind.IsDir() && !kind.IsRegular():
			return fmt.Errorf("%s is not a regular file or directory; uploads copy only regular files and directories", u.localPath(name))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		e := uploadEntry{name: name, dir: entry.IsDir(), mode: info.Mode().Perm()}
		if !e.dir {
			e.size = info.Size()
		}
		if e.header, err = headerBytes(e.tarHeader()); err != nil {
			return fmt.Errorf("%s: %w", u.localPath(name), err)
		}
		if len(newPaths(e.name, nil)) > u.limits.Entries {
			return fmt.Errorf("%s is nested too deeply to upload: one call takes at most %d files and directories", u.localPath(e.name), u.limits.Entries)
		}
		u.entries = append(u.entries, e)
		return nil
	})
}

// setAsideLargeFiles moves the files too large for one archive call, in walk
// order, to those sent in parts.
func (u *Upload) setAsideLargeFiles() error {
	p, err := newPacker(u)
	if err != nil {
		return err
	}
	kept := u.entries[:0]
	for _, e := range u.entries {
		large, err := u.tooLargeForAnArchive(p, e)
		if err != nil {
			return err
		}
		if !large {
			kept = append(kept, e)
			continue
		}
		if e.size > MaxPartedBytes {
			return fmt.Errorf("%s is too large to upload: files are limited to %s", u.localPath(e.name), limitSize(MaxPartedBytes))
		}
		u.large = append(u.large, e)
	}
	u.entries = kept
	return nil
}

// tooLargeForAnArchive reports whether e cannot fit an archive by itself. A
// file that might not fit even compressed is compressed to find out.
func (u *Upload) tooLargeForAnArchive(p *packer, e uploadEntry) (bool, error) {
	switch {
	case e.dir:
		return false, nil
	case e.expanded()+tarTrailer > u.limits.Expanded:
		return true, nil
	case worstCase(e)+p.framing() <= u.limits.Body:
		return false, nil
	}
	_, err := p.segment(e, io.Discard, 0)
	if errors.Is(err, errSegmentTooLarge) {
		return true, nil
	}
	return false, err
}

// Each packs the archives in order and passes each gzip-compressed tar to
// send, stopping at the first error. The archive's memory is reused once send
// returns. Archives leave out LargeFiles. A directory with nothing else in it
// still makes one empty archive, which creates the destination.
func (u *Upload) Each(send func(archive []byte) error) error {
	p, err := newPacker(u)
	if err != nil {
		return err
	}
	var segment bytes.Buffer
	c := newChunk()
	for _, e := range u.entries {
		if c.fits(p, e, worstCase(e)) {
			if err := p.continueStream(c, e); err != nil {
				return err
			}
			continue
		}
		// Near the archive's end, compress the entry on its own to learn
		// whether it still fits.
		segment.Reset()
		sums, err := p.segment(e, &segment, c.crc)
		if errors.Is(err, errSegmentTooLarge) {
			return u.changed(e.name)
		}
		if err != nil {
			return err
		}
		crc := sums.chained
		if !c.fits(p, e, int64(segment.Len())) {
			if err := send(p.finish(c)); err != nil {
				return err
			}
			c.reset()
			crc = sums.fresh
			// OpenUpload set aside every entry that cannot fit one archive.
			if !c.fits(p, e, int64(segment.Len())) {
				return u.changed(e.name)
			}
		}
		c.add(e, segment.Bytes(), crc)
		// The archive's stream did not see this entry, so it cannot refer
		// back across it.
		c.restart = true
	}
	return send(p.finish(c))
}

// packer gzip-compresses tar entries. An entry sure to fit the archive
// continues its deflate stream. Otherwise it is compressed on its own, with a
// fresh compressor, so its exact size is known before choosing the archive it
// joins: it never refers to data before it, so it is valid at the end of the
// current archive or the start of the next. Every entry ends with a sync flush,
// so the archive's size is always exact.
type packer struct {
	upload *Upload
	stream *flate.Writer
	single *flate.Writer
	// tail ends every archive: the tar trailer as the final deflate block.
	tail []byte
}

func newPacker(u *Upload) (*packer, error) {
	var tail bytes.Buffer
	single, err := flate.NewWriter(&tail, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := single.Write(make([]byte, tarTrailer)); err != nil {
		return nil, err
	}
	if err := single.Close(); err != nil {
		return nil, err
	}
	stream, err := flate.NewWriter(io.Discard, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	return &packer{upload: u, stream: stream, single: single, tail: tail.Bytes()}, nil
}

// framing is the bytes an archive carries besides its entries.
func (p *packer) framing() int64 {
	return int64(len(gzipHeader) + len(p.tail) + 8)
}

// worstCase bounds an entry's compressed size. Deflate stores a block it
// cannot shrink; a block it cannot store costs at most its Huffman tables.
func worstCase(e uploadEntry) int64 {
	raw := e.expanded()
	return raw + raw>>4 + 4096
}

// continueStream continues the archive's deflate stream with e.
func (p *packer) continueStream(c *chunk, e uploadEntry) error {
	if c.restart {
		p.stream.Reset(&c.data)
		c.restart = false
	}
	sums := &crcWriter{chained: c.crc}
	if err := p.compress(e, p.stream, sums); err != nil {
		return err
	}
	c.add(e, nil, sums.chained)
	return nil
}

// segmentSums are an entry's CRC-32s: chained continues the archive the entry
// was packed after, fresh starts a new one.
type segmentSums struct{ chained, fresh uint32 }

// errSegmentTooLarge is an entry that does not fit an archive by itself.
var errSegmentTooLarge = errors.New("archive entry exceeds the body limit")

// segment compresses e on its own to out, refusing with errSegmentTooLarge an
// entry that could not fit an archive by itself.
func (p *packer) segment(e uploadEntry, out io.Writer, chained uint32) (segmentSums, error) {
	bounded := &cappedWriter{w: out, left: p.upload.limits.Body - p.framing()}
	p.single.Reset(bounded)
	sums := &crcWriter{chained: chained}
	err := p.compress(e, p.single, sums)
	if bounded.exceeded {
		return segmentSums{}, errSegmentTooLarge
	}
	return segmentSums{chained: sums.chained, fresh: sums.fresh}, err
}

// compress writes e's tar header, content and padding to w and flushes it.
func (p *packer) compress(e uploadEntry, w *flate.Writer, sums *crcWriter) error {
	tw := tar.NewWriter(io.MultiWriter(sums, w))
	if err := p.writeEntry(tw, e); err != nil {
		return err
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return w.Flush()
}

func (p *packer) writeEntry(tw *tar.Writer, e uploadEntry) error {
	if err := tw.WriteHeader(e.tarHeader()); err != nil {
		return err
	}
	if e.dir {
		return nil
	}
	file, err := p.upload.open(e)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := io.CopyN(tw, file, e.size); err != nil {
		if errors.Is(err, io.EOF) {
			return p.upload.changed(e.name)
		}
		return err
	}
	return nil
}

// open opens e's file, refusing one that is no longer what the walk saw.
func (u *Upload) open(e uploadEntry) (*os.File, error) {
	file, err := u.root.Open(e.name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() != e.size) {
		err = u.changed(e.name)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (u *Upload) changed(name string) error {
	return fmt.Errorf("%s changed while it was being uploaded; upload again", u.localPath(name))
}

// chunk is one archive being filled. Its memory is reused for the next.
type chunk struct {
	data     bytes.Buffer
	crc      uint32
	expanded int64
	paths    map[string]struct{}
	// restart is set when the packer's stream must start afresh.
	restart bool
}

func newChunk() *chunk {
	c := &chunk{paths: map[string]struct{}{}}
	c.reset()
	return c
}

func (c *chunk) reset() {
	c.data.Reset()
	c.data.Write(gzipHeader)
	c.crc, c.expanded = 0, 0
	clear(c.paths)
	c.restart = true
}

// fits reports whether e, compressed to size bytes, fits the archive.
func (c *chunk) fits(p *packer, e uploadEntry, size int64) bool {
	limits := p.upload.limits
	return len(c.paths)+len(newPaths(e.name, c.paths)) <= limits.Entries &&
		c.expanded+e.expanded()+tarTrailer <= limits.Expanded &&
		int64(c.data.Len())+size+int64(len(p.tail))+8 <= limits.Body
}

// add records e, whose compressed bytes are segment or already in data.
func (c *chunk) add(e uploadEntry, segment []byte, crc uint32) {
	for _, name := range newPaths(e.name, c.paths) {
		c.paths[name] = struct{}{}
	}
	c.data.Write(segment)
	c.crc = crc
	c.expanded += e.expanded()
}

// finish ends the archive with the tar trailer and the gzip trailer.
func (p *packer) finish(c *chunk) []byte {
	c.data.Write(p.tail)
	c.crc = crc32.Update(c.crc, crc32.IEEETable, make([]byte, tarTrailer))
	c.expanded += tarTrailer
	c.data.Write(binary.LittleEndian.AppendUint32(nil, c.crc))
	c.data.Write(binary.LittleEndian.AppendUint32(nil, gzipSize(c.expanded)))
	return c.data.Bytes()
}

// gzipSize is gzip's ISIZE: the uncompressed size modulo 2^32.
func gzipSize(n int64) uint32 {
	return uint32(n & math.MaxUint32)
}

// newPaths returns name and the directories it implies that are not in seen,
// which is how the API counts an archive's files and directories.
func newPaths(name string, seen map[string]struct{}) []string {
	var paths []string
	for p := name; p != "."; p = path.Dir(p) {
		if _, ok := seen[p]; ok {
			break
		}
		paths = append(paths, p)
	}
	return paths
}

func (e uploadEntry) tarHeader() *tar.Header {
	if e.dir {
		return &tar.Header{Typeflag: tar.TypeDir, Name: e.name + "/", Mode: int64(e.mode)}
	}
	return &tar.Header{Typeflag: tar.TypeReg, Name: e.name, Mode: int64(e.mode), Size: e.size}
}

// expanded is the entry's uncompressed tar bytes.
func (e uploadEntry) expanded() int64 {
	return e.header + (e.size+tarBlock-1)/tarBlock*tarBlock
}

func (u *Upload) localPath(name string) string {
	return filepath.Join(u.directory, filepath.FromSlash(name))
}

// headerBytes is the size of h as written, extended headers included.
func headerBytes(h *tar.Header) (int64, error) {
	var written bytes.Buffer
	if err := tar.NewWriter(&written).WriteHeader(h); err != nil {
		return 0, err
	}
	return int64(written.Len()), nil
}

func limitSize(n int64) string {
	if n >= 1<<30 && n%(1<<30) == 0 {
		return fmt.Sprintf("%d GiB", n>>30)
	}
	if n >= 1<<20 && n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

// cappedWriter fails a write past its limit instead of buffering it.
type cappedWriter struct {
	w        io.Writer
	left     int64
	exceeded bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		w.exceeded = true
		return 0, errors.New("archive segment exceeds its limit")
	}
	n, err := w.w.Write(p)
	w.left -= int64(n)
	return n, err
}

type crcWriter struct{ chained, fresh uint32 }

func (w *crcWriter) Write(p []byte) (int, error) {
	w.chained = crc32.Update(w.chained, crc32.IEEETable, p)
	w.fresh = crc32.Update(w.fresh, crc32.IEEETable, p)
	return len(p), nil
}
