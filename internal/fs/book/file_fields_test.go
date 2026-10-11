package book

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func testFieldFileStat(t *testing.T, mode uint32) *proto.Stat {
	return newStat(util.NewTestFS(t), "test", mode)
}

func TestFieldFileGetUpdatesOnReopen(t *testing.T) {
	value := "first"
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return value }, nil)

	fid := fstest.Fid(t, ff, 1)
	if got := fid.Get(proto.Mode(0), 10); got != "first\n" {
		t.Errorf("Read = %q, want %q", got, "first\n")
	}

	fid.Close()
	value = "second"

	if got := fid.Get(proto.Mode(0), 10); got != "second\n" {
		t.Errorf("Read after reopen = %q, want %q", got, "second\n")
	}
}

func TestFieldFileRead(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"value", "hello", "hello\n"},
		{"empty", "", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return tc.value }, nil)

			fstest.StatLength(t, ff, uint64(len(tc.want)))
			if got := fstest.Fid(t, ff, 1).Get(proto.Mode(0), 10); got != tc.want {
				t.Errorf("Read = %q, want %q", got, tc.want)
			}
		})
	}
}

// WriteBuffer owns the offsets. This pins only that the field seeds the buffer
// with its value unless the open carries Otrunc. Each row writes at offset 4,
// the end of the snapshot "old\n". A write at offset 0 would hide the
// difference, since a short first write there truncates either way.
func TestFieldFileOtrunc(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode proto.Mode
		want string
	}{
		{"Otrunc drops the value", proto.Otrunc, "\x00\x00\x00\x00new"},
		{"no Otrunc appends", proto.Mode(0), "old\nnew"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "old" }, func(s string) error {
				got = s
				return nil
			})

			fid := fstest.Fid(t, ff, 1)
			fid.Open(tc.mode)
			fid.Write(4, "new\n")
			fid.Close()

			if got != tc.want {
				t.Errorf("set was called with %q, want %q", got, tc.want)
			}
		})
	}
}
