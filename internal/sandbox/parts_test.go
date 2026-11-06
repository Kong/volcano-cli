package sandbox

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openUpload(t *testing.T, dir string, limits UploadLimits) *Upload {
	t.Helper()
	upload, err := OpenUpload(dir, limits)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, upload.Close()) })
	return upload
}

func TestUploadSetsAsideFilesTooLargeForOneArchive(t *testing.T) {
	t.Parallel()
	limits := UploadLimits{Body: 16 << 10, Entries: 100, Expanded: 64 << 10}
	dir := writeTree(t, map[string]string{
		"a/noise":    randomBytes(t, 20<<10),         // over the body limit even compressed
		"a/small":    "small",                        // fits
		"b/zeros":    strings.Repeat("\x00", 40<<10), // compresses to fit
		"c/expanded": strings.Repeat("\x00", 64<<10), // over the uncompressed limit
		"d/last":     "last",                         // fits
	})
	upload := openUpload(t, dir, limits)

	large := upload.LargeFiles()
	require.Len(t, large, 2)
	assert.Equal(t, "a/noise", large[0].Name)
	assert.Equal(t, filepath.Join(dir, "a", "noise"), large[0].Path)
	assert.Equal(t, int64(20<<10), large[0].Size)
	assert.Equal(t, "c/expanded", large[1].Name)

	var names []string
	require.NoError(t, upload.Each(func(archive []byte) error {
		for _, e := range readArchive(t, archive, limits) {
			names = append(names, e.name)
		}
		return nil
	}))
	assert.Equal(t, []string{"a", "a/small", "b", "b/zeros", "c", "d", "d/last"}, names,
		"archives keep every directory and leave out the large files")
}

func TestLargeFilePartsCoverTheFileInOrder(t *testing.T) {
	t.Parallel()
	content := randomBytes(t, 2*PartSize+12345)
	dir := writeTree(t, map[string]string{"big.bin": content})
	upload := openUpload(t, dir, UploadLimits{Body: 16 << 10, Entries: 100, Expanded: 1 << 20})
	large := upload.LargeFiles()
	require.Len(t, large, 1)

	sum := sha256.Sum256([]byte(content))
	var offsets []int64
	var got bytes.Buffer
	require.NoError(t, large[0].Parts(func(part FilePart) error {
		offsets = append(offsets, part.Offset)
		assert.Equal(t, int64(len(content)), part.Size)
		assert.Equal(t, hex.EncodeToString(sum[:]), part.SHA256)
		assert.LessOrEqual(t, len(part.Data), PartSize)
		assert.Equal(t, int64(got.Len()), part.Offset)
		got.Write(part.Data)
		return nil
	}))
	assert.Equal(t, []int64{0, PartSize, 2 * PartSize}, offsets)
	assert.Equal(t, content, got.String())
}

func TestLargeFilePartsStopAtTheFirstSendError(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"big.bin": randomBytes(t, PartSize+1)})
	large := openUpload(t, dir, UploadLimits{Body: 16 << 10, Entries: 100, Expanded: 1 << 20}).LargeFiles()
	require.Len(t, large, 1)
	refused := errors.New("refused")
	sent := 0
	err := large[0].Parts(func(FilePart) error {
		sent++
		return refused
	})
	require.ErrorIs(t, err, refused)
	assert.Equal(t, 1, sent)
}

func TestLargeFilePartsNoticeAFileThatChanged(t *testing.T) {
	t.Parallel()
	dir := writeTree(t, map[string]string{"big.bin": randomBytes(t, 20<<10)})
	large := openUpload(t, dir, UploadLimits{Body: 16 << 10, Entries: 100, Expanded: 1 << 20}).LargeFiles()
	require.Len(t, large, 1)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), []byte("shorter"), 0o600))
	err := large[0].Parts(func(FilePart) error { return nil })
	require.ErrorContains(t, err, "big.bin changed while it was being uploaded")
}
