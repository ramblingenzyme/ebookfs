package book

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

// The written bytes reach the library as a cover edit, and the library's
// refusal of bytes that are no image comes back from the clunk.
func TestCoverFileWrite(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewGray(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		write   string
		refused bool
	}{
		{"replaces", jpg.String(), false},
		{"refusal reaches the clunk", "not an image", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lib, b := ingestedBook(t)

			cf := newCoverFile(newStat(util.NewTestFS(t), "cover.jpg", 0644), lib, lib.Edit, util.Fixed(b))
			fid := fstest.Fid(t, cf, 1)
			fid.Open(proto.Mode(0))
			fid.Write(0, tc.write)

			if tc.refused {
				if err := fid.WantCloseError(); err == nil {
					t.Error("Close succeeded, want the library's refusal")
				}
				return
			}
			fid.Close()

			r, err := lib.Content(b.ID())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if got, err := r.Cover(); err != nil || string(got) != tc.write {
				t.Errorf("cover = %d bytes (err %v), want the %d written", len(got), err, len(tc.write))
			}
		})
	}
}
