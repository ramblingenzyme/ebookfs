// A ctl command, written the way a 9P client writes one, driving the real
// registry and the real views. Every layer between the command line and the
// served tree is the production one; only the library is faked, since the tree
// is what these assert on.
//
// The unit tests in fs/ctl drive dispatch with a fake and assert on the string
// it returns. Nothing there proves a command reaches the tree, which is the
// half that has historically broken.

package fs

import (
	"strings"
	"testing"

	"github.com/knusbaum/go9p/fs"
	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/ctl"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// runCtl writes cmd to the ctl file and clunks it, which is what executes the
// command. It returns the line the command log recorded.
func runCtl(t *testing.T, cf *ctl.CtlFile, log *ctl.CommandLog, cmd string) string {
	t.Helper()
	fid := fstest.Fid(t, cf, 1)
	fid.Write(0, cmd)
	fid.Close()
	entries := log.Entries()
	if len(entries) == 0 {
		t.Fatalf("%q recorded nothing in the log", cmd)
	}
	return entries[len(entries)-1].Result
}

// ctlTree wires ctl onto a registry serving the views a command can move a book
// between, with the library's Edit applying the edits to the test's own book.
func ctlTree(t *testing.T, cur *bookmodel.Book) (*ctl.CtlFile, *ctl.CommandLog, map[string]fs.Dir) {
	t.Helper()
	f := newTestFS(t)

	search := mock.SearchDeleter{
		SearchFn: func(library.Query) ([]*library.Book, error) {
			return []*library.Book{util.WrapBook(cur)}, nil
		},
	}
	edit := mock.Editor{
		EditFn: func(_ int64, e library.Edits) (*library.Book, error) {
			next := *cur
			if e.Status != nil {
				next.Meta.Status = *e.Status
			}
			if e.Tags != nil {
				next.Meta.Tags = *e.Tags
			}
			if e.Authors != nil {
				next.Authors = *e.Authors
			}
			cur = &next
			return util.WrapBook(cur), nil
		},
	}

	reg := registry.NewBookRegistry(f, edit)
	dirs := map[string]fs.Dir{
		"books":     views.NewAllBooksDir(reg),
		"by-author": views.NewByAuthorDir(reg),
		"by-tag":    views.NewByTagDir(reg),
		"by-status": views.NewByStatusDir(reg),
	}
	reg.Add(util.WrapBook(cur))

	log := ctl.NewCommandLog(16)
	return ctl.NewCtlFile(f, search, reg, log), log, dirs
}

// group reports the entries under dirs[view]/key, and whether that group exists.
func group(t *testing.T, dirs map[string]fs.Dir, view, key string) ([]string, bool) {
	t.Helper()
	child, ok := dirs[view].Children()[key]
	if !ok {
		return nil, false
	}
	return fstest.ChildNames(child.(fs.Dir)), true
}

func TestCtlSetStatusMovesTheBookInByStatus(t *testing.T) {
	b := makeBook(1, "Test", "Alice")
	b.Meta.Status = "unread"
	cf, log, dirs := ctlTree(t, b)

	if got := runCtl(t, cf, log, "set-status reading 1"); !strings.HasPrefix(got, "ok:") {
		t.Fatalf("set-status: %s", got)
	}

	if _, ok := group(t, dirs, "by-status", "unread"); ok {
		t.Error("by-status/unread survived the move; the old group was not pruned")
	}
	names, ok := group(t, dirs, "by-status", "reading")
	if !ok {
		t.Fatal("by-status/reading was never created")
	}
	if len(names) != 1 || names[0] != "Test" {
		t.Errorf("by-status/reading = %v, want the book filed under its title", names)
	}
}

func TestCtlAddTagCreatesTheTagGroup(t *testing.T) {
	cf, log, dirs := ctlTree(t, makeBook(1, "Test", "Alice"))

	if _, ok := group(t, dirs, "by-tag", "favourite"); ok {
		t.Fatal("by-tag/favourite existed before the command")
	}
	if got := runCtl(t, cf, log, "add-tag favourite 1"); !strings.HasPrefix(got, "ok:") {
		t.Fatalf("add-tag: %s", got)
	}

	names, ok := group(t, dirs, "by-tag", "favourite")
	if !ok {
		t.Fatal("by-tag/favourite was never created")
	}
	if len(names) != 1 {
		t.Errorf("by-tag/favourite = %v, want the tagged book", names)
	}
}

func TestCtlRenameAuthorRehomesInByAuthor(t *testing.T) {
	cf, log, dirs := ctlTree(t, makeBook(1, "Test", "Alice"))

	if got := runCtl(t, cf, log, `rename-author "Alice" "Bob"`); !strings.HasPrefix(got, "ok:") {
		t.Fatalf("rename-author: %s", got)
	}

	if _, ok := group(t, dirs, "by-author", "Alice"); ok {
		t.Error("by-author/Alice survived the rename")
	}
	if _, ok := group(t, dirs, "by-author", "Bob"); !ok {
		t.Error("by-author/Bob was never created")
	}
}

// An unknown command must reach the log and leave the tree alone, since the
// operator's only feedback is that log line.
func TestCtlUnknownCommandLeavesTheTreeAlone(t *testing.T) {
	cf, log, dirs := ctlTree(t, makeBook(1, "Test", "Alice"))
	before := fstest.ChildNames(dirs["books"])

	got := runCtl(t, cf, log, "explode 1")
	if !strings.Contains(got, "unknown command") {
		t.Errorf("log recorded %q, want an unknown-command error", got)
	}
	fstest.ChildCount(t, dirs["books"], len(before))
}
