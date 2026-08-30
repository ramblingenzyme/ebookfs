package book

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

// newTestBookDir builds a BookDir over a fresh FS with a no-op edit callback —
func newTestBookDir(t *testing.T, b *library.Book) *BookDir {
	t.Helper()
	return NewBookDir(testutil.NewTestFS(t), libfake.Lib{}, func(int64, library.Edits) error { return nil }, b)
}

func TestNewBookDirCreatesCoverChild(t *testing.T) {
	b := testutil.MakeMutableBook(1, "Has Cover", "Author")
	b.CoverPath = "OEBPS/cover.jpg"

	d := newTestBookDir(t, testutil.WrapBook(b))

	if _, ok := d.Children()["cover.jpg"]; !ok {
		t.Error("BookDir should contain 'cover.jpg' when CoverPath is set")
	}
}

func TestNewBookDirNoCoverWhenEmpty(t *testing.T) {
	b := testutil.MakeMutableBook(1, "No Cover", "Author")
	b.CoverPath = ""

	d := newTestBookDir(t, testutil.WrapBook(b))

	if _, ok := d.Children()["cover.jpg"]; ok {
		t.Error("BookDir should not contain 'cover.jpg' when CoverPath is empty")
	}
}

func TestBookDirStatReportsTitle(t *testing.T) {
	d := newTestBookDir(t, testutil.MakeBook(1, "My Title", "Author"))

	s := d.Stat()
	if s.Name != "My Title" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "My Title")
	}
}

func TestBookDirHasIDChild(t *testing.T) {
	d := newTestBookDir(t, testutil.MakeBook(1, "Test", "Author"))

	child := d.Children()["id"]
	if child == nil {
		t.Fatal("BookDir should have 'id' child")
	}
	if _, ok := child.(*fs.StaticFile); !ok {
		t.Errorf("'id' child should be a StaticFile")
	}
}

// readChild opens a child file and reads it whole, the way a client cat'ing it
// would.
func readChild(t *testing.T, d *BookDir, name string) string {
	t.Helper()
	child, ok := d.Children()[name]
	if !ok {
		t.Fatalf("BookDir should have a %q child", name)
	}
	f, ok := child.(fs.File)
	if !ok {
		t.Fatalf("%q child is not a file", name)
	}
	const fid = 1
	if err := f.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open(%s): %v", name, err)
	}
	data, err := f.Read(fid, 0, 4096)
	if err != nil {
		t.Fatalf("Read(%s): %v", name, err)
	}
	return string(data)
}

// TestBookDirIdentifiersFile pins the rendering, including the sort: a map has
// no order, and a file that shuffles between reads is no use to a diff.
func TestBookDirIdentifiersFile(t *testing.T) {
	b := testutil.MakeMutableBook(1, "Test", "Author")
	b.Identifiers = map[string]string{
		"uuid": "a1b2c3d4",
		"isbn": "9780123456789",
		"doi":  "10.1234/beta",
		// isbn-a sorts after isbn by scheme but before it by rendered line,
		// since "=" sorts after "-". Both come out of one ONIX code list, so
		// this pair is what tells the two orderings apart.
		"isbn-a": "10.978.12345/99990",
	}

	d := newTestBookDir(t, testutil.WrapBook(b))

	want := "doi=10.1234/beta\nisbn=9780123456789\nisbn-a=10.978.12345/99990\nuuid=a1b2c3d4\n"
	if got := readChild(t, d, "identifiers"); got != want {
		t.Errorf("identifiers = %q, want %q", got, want)
	}
}

// A book with none still gets the file, the way an unset pubdate does: present
// and empty, so a client never has to tell "no identifiers" from "no such file".
func TestBookDirIdentifiersFileEmpty(t *testing.T) {
	d := newTestBookDir(t, testutil.MakeBook(1, "Test", "Author"))

	if got := readChild(t, d, "identifiers"); got != "\n" {
		t.Errorf("identifiers = %q, want a lone newline", got)
	}
}
