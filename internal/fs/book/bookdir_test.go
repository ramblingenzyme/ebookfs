package book

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

func newTestBookDir(t *testing.T, b *library.Book) *BookDir {
	t.Helper()
	return NewBookDir(util.NewTestFS(t), mock.ContentReader{}, func(int64, library.Edits) (*library.Book, error) { return nil, nil }, b)
}

func TestBookDirListing(t *testing.T) {
	fields := []string{"authors", "contributors", "description", "id", "identifiers", "language",
		"opf", "pubdate", "publisher", "rating", "rights", "series", "series_index", "status",
		"subjects", "tags", "title"}
	for _, tc := range []struct {
		name  string
		cover string
		extra []string
	}{
		{"with a cover", "OEBPS/cover.jpg", []string{"cover.jpg"}},
		{"without", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := util.MakeMutableBook(1, "Title", "Author")
			b.CoverPath = tc.cover
			b.EpubPath = "Author/Title (1)/Title - Author.epub"

			want := append([]string{"Title - Author.epub"}, fields...)
			want = append(want, tc.extra...)
			slices.Sort(want)
			if got := fstest.ChildNames(newTestBookDir(t, util.WrapBook(b))); !slices.Equal(got, want) {
				t.Errorf("children = %q, want %q", got, want)
			}
		})
	}
}

// The rendering, including the sort: a map has no order, and a file that
// shuffles between reads is no use to a diff.
func TestBookDirIdentifiersFile(t *testing.T) {
	b := util.MakeMutableBook(1, "Test", "Author")
	b.Identifiers = map[string]string{
		"uuid": "a1b2c3d4",
		"isbn": "9780123456789",
		"doi":  "10.1234/beta",
		// isbn-a sorts after isbn by scheme but before it by rendered line,
		// since "=" sorts after "-". Both come out of one ONIX code list, so
		// this pair is what tells the two orderings apart.
		"isbn-a": "10.978.12345/99990",
	}

	d := newTestBookDir(t, util.WrapBook(b))

	want := "doi=10.1234/beta\nisbn=9780123456789\nisbn-a=10.978.12345/99990\nuuid=a1b2c3d4\n"
	if got := readChild(t, d, "identifiers"); got != want {
		t.Errorf("identifiers = %q, want %q", got, want)
	}

	// A book with no identifiers still gets the file, the way an unset pubdate
	// does, so a client never has to tell "no identifiers" from "no such file".
	empty := newTestBookDir(t, util.MakeBook(2, "Test", "Author"))
	if got := readChild(t, empty, "identifiers"); got != "\n" {
		t.Errorf("identifiers with none = %q, want a lone newline", got)
	}
}

// readChild opens a child file and reads it whole, the way a client cat'ing it
// would.
func readChild(t *testing.T, d *BookDir, name string) string {
	t.Helper()
	return fstest.Fid(t, fstest.ChildAs[fs.File](t, d, name), 1).Get(proto.Mode(0), 4096)
}
