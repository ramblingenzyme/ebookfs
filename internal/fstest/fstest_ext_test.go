// The helpers are driven the way a test drives them, against go9p's own
// StaticFile and StaticDir. Only the passing paths are covered: asserting what
// a helper prints when it fails would need a fake testing.TB, and the failure
// messages are read by a human at the moment they fire, not by a test.

package fstest_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
)

func newFS(t *testing.T) *fs.FS {
	t.Helper()
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f
}

// staticFile returns a file holding data, addressed the way the served tree
// addresses one.
func staticFile(t *testing.T, name, data string) *fs.StaticFile {
	t.Helper()
	f := newFS(t)
	return fs.NewStaticFile(f.NewStat(name, "glenda", "glenda", 0644), []byte(data))
}

func staticDir(t *testing.T, children ...string) *fs.StaticDir {
	t.Helper()
	f := newFS(t)
	d := fs.NewStaticDir(f.NewStat("d", "glenda", "glenda", 0555|proto.DMDIR))
	for _, name := range children {
		if err := d.AddChild(fs.NewStaticFile(f.NewStat(name, "glenda", "glenda", 0644), nil)); err != nil {
			t.Fatalf("AddChild %q: %v", name, err)
		}
	}
	return d
}

func TestReadReturnsTheContent(t *testing.T) {
	f := staticFile(t, "x", "hello")

	if got := fstest.Fid(t, f, 1).Get(proto.Mode(0), 16); got != "hello" {
		t.Errorf("Get = %q, want %q", got, "hello")
	}
}

// Two handles on one file address it independently. Tests that exist to prove
// per-fid isolation depend on this.
func TestTwoFidsAreIndependent(t *testing.T) {
	f := staticFile(t, "x", "hello")

	fid1, fid2 := fstest.Fid(t, f, 1), fstest.Fid(t, f, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))
	fid1.Close()

	if got := fid2.Read(0, 16); got != "hello" {
		t.Errorf("fid 2 read %q after fid 1 clunked, want %q", got, "hello")
	}
}

// fidFile reads only on a fid that is open, which is how every file in this
// tree behaves and how go9p's StaticFile does not.
type fidFile struct {
	fs.File
	open     map[uint64]bool
	closeErr error
}

func (f *fidFile) Open(fid uint64, _ proto.Mode) error {
	f.open[fid] = true
	return nil
}

func (f *fidFile) Read(fid, _, _ uint64) ([]byte, error) {
	if !f.open[fid] {
		return nil, errors.New("not open")
	}
	return []byte("hello"), nil
}

func (f *fidFile) Close(fid uint64) error {
	delete(f.open, fid)
	return f.closeErr
}

// CloseErr hands the clunk error back rather than failing the test, which is
// what a pass-through helper and a goroutine both need.
func TestCloseErrReturnsTheError(t *testing.T) {
	want := errors.New("commit rejected")
	f := &fidFile{File: staticFile(t, "x", "hello"), open: map[uint64]bool{}, closeErr: want}

	fid := fstest.Fid(t, f, 1)
	fid.Open(proto.Mode(0))
	if got := fid.CloseErr(); got != want {
		t.Errorf("CloseErr = %v, want %v", got, want)
	}
}

func TestWantReadErrorAcceptsAClunkedFid(t *testing.T) {
	f := &fidFile{File: staticFile(t, "x", "hello"), open: map[uint64]bool{}}

	fid := fstest.Fid(t, f, 1)
	fid.Open(proto.Mode(0))
	if got := fid.Read(0, 16); got != "hello" {
		t.Fatalf("read before clunk = %q, want the content", got)
	}
	fid.Close()
	fid.WantReadError()
}

func TestChildNamesSorts(t *testing.T) {
	d := staticDir(t, "charlie", "alpha", "bravo")

	want := []string{"alpha", "bravo", "charlie"}
	if got := fstest.ChildNames(d); !slices.Equal(got, want) {
		t.Errorf("ChildNames = %v, want %v", got, want)
	}
}

func TestChildAssertions(t *testing.T) {
	d := staticDir(t, "present")

	fstest.HasChild(t, d, "present")
	fstest.NoChild(t, d, "absent")
	fstest.ChildCount(t, d, 1)
}

// The type argument resolves in the calling package, which is how this reaches
// types fstest cannot name. Here it is an exported one; the views tests use it
// on their own unexported directory types.
func TestChildAsAssertsTheType(t *testing.T) {
	d := staticDir(t, "leaf")

	if got := fstest.ChildAs[*fs.StaticFile](t, d, "leaf"); got == nil {
		t.Error("ChildAs returned nil for a child that is a StaticFile")
	}
	if got := fstest.ChildAs[fs.File](t, d, "leaf"); got == nil {
		t.Error("ChildAs returned nil for an interface type argument")
	}
}

func TestStatLength(t *testing.T) {
	fstest.StatLength(t, staticFile(t, "x", "hello"), 5)
}
