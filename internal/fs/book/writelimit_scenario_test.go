// coverFile and fieldFile are the two writable files, and both cap writes
// through the same vfile.WriteBuffer. This pins that shared cap for both at
// once: an offset past the limit is rejected, a near-maxuint64 offset cannot
// wrap past the check, and a write ending exactly at the limit is allowed.
//
// Neither file owns the rule; the buffer does, so a cap holding for one file
// only would be a bug.

package book

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
)

// limitedWriteFile is the common surface of the two size-capped writable files.
type limitedWriteFile interface {
	Open(fid uint64, mode proto.Mode) error
	Write(fid uint64, offset uint64, data []byte) (uint32, error)
}

func TestWriteFileSizeLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit uint64
		open  func(t *testing.T) limitedWriteFile
	}{
		{"coverFile", maxCoverFileSize, func(t *testing.T) limitedWriteFile {
			b := testutil.MakeBook(1, "Test", "Author")
			return newCoverFile(newStat(testutil.NewTestFS(t), "cover.jpg", 0644), contentReader{}, func(int64, library.Edits) error { return nil }, testutil.Fixed(b))
		}},
		{"fieldFile", maxFieldFileSize, func(t *testing.T) limitedWriteFile {
			return newFieldFile(newStat(testutil.NewTestFS(t), "field", 0644), func() string { return "" }, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("exceeds limit rejected", func(t *testing.T) {
				f := tc.open(t)
				f.Open(1, proto.Mode(0))
				if _, err := f.Write(1, tc.limit, []byte("x")); err == nil {
					t.Fatal("expected error writing past the size limit")
				}
			})
			t.Run("offset overflow rejected", func(t *testing.T) {
				f := tc.open(t)
				f.Open(1, proto.Mode(0))
				if _, err := f.Write(1, ^uint64(0)-3, []byte("overflow")); err == nil {
					t.Fatal("expected error on overflowing write offset")
				}
			})
			t.Run("write at limit allowed", func(t *testing.T) {
				f := tc.open(t)
				f.Open(1, proto.Mode(0))
				if _, err := f.Write(1, tc.limit-4, []byte("test")); err != nil {
					t.Errorf("write ending at the limit should succeed, got: %v", err)
				}
			})
		})
	}
}
