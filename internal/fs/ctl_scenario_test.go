// ctl commands drive the public library API; its subscribed registry updates
// the 9P views after each committed edit.
package fs

import (
	"strings"
	"testing"

	go9pfs "github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/ctl"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestCtlSetStatusMovesTheBookInByStatus(t *testing.T) {
	book := util.MakeMutableBook(1, "Test", "Alice")
	book.Meta.Status = "unread"
	cf, log, dirs := ctlTree(t, book)

	if got := runCtl(t, cf, log, "set-status reading 1"); !strings.HasPrefix(got, "ok:") {
		t.Fatalf("set-status: %s", got)
	}
	if _, ok := group(t, dirs, "by-status", "unread"); ok {
		t.Error("by-status/unread survived the move")
	}
	names, ok := group(t, dirs, "by-status", "reading")
	if !ok || len(names) != 1 || names[0] != "Test" {
		t.Errorf("by-status/reading = %v, want Test", names)
	}
}

func TestCtlAddTagCreatesTheTagGroup(t *testing.T) {
	cf, log, dirs := ctlTree(t, util.MakeMutableBook(1, "Test", "Alice"))
	if _, ok := group(t, dirs, "by-tag", "favourite"); ok {
		t.Fatal("by-tag/favourite existed before the command")
	}
	if got := runCtl(t, cf, log, "add-tag favourite 1"); !strings.HasPrefix(got, "ok:") {
		t.Fatalf("add-tag: %s", got)
	}
	if names, ok := group(t, dirs, "by-tag", "favourite"); !ok || len(names) != 1 {
		t.Errorf("by-tag/favourite = %v, want the tagged book", names)
	}
}

func TestCtlRenameAuthorRehomesInByAuthor(t *testing.T) {
	cf, log, dirs := ctlTree(t, util.MakeMutableBook(1, "Test", "Alice"))
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

func TestCtlUnknownCommandLeavesTheTreeAlone(t *testing.T) {
	cf, log, dirs := ctlTree(t, util.MakeMutableBook(1, "Test", "Alice"))
	before := fstest.ChildNames(dirs["books"])

	got := runCtl(t, cf, log, "explode 1")
	if !strings.Contains(got, "unknown command") {
		t.Errorf("log recorded %q, want an unknown-command error", got)
	}
	fstest.ChildCount(t, dirs["books"], len(before))
}

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

func ctlTree(t *testing.T, initial *book.Book) (*ctl.CtlFile, *ctl.CommandLog, map[string]go9pfs.Dir) {
	t.Helper()
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	viewsByName := map[string]go9pfs.Dir{
		"books":     views.NewAllBooksDir(reg),
		"by-author": views.NewByAuthorDir(reg),
		"by-tag":    views.NewByTagDir(reg),
		"by-status": views.NewByStatusDir(reg),
	}
	lib.AddHook(reg)
	ingestRegistryScenarioBook(t, lib, initial.Title, initial.Authors[0].Name)

	log := ctl.NewCommandLog(16)
	return ctl.NewCtlFile(f, lib, log), log, viewsByName
}

func group(t *testing.T, dirs map[string]go9pfs.Dir, view, key string) ([]string, bool) {
	t.Helper()
	child, ok := dirs[view].Children()[key]
	if !ok {
		return nil, false
	}
	return fstest.ChildNames(child.(go9pfs.Dir)), true
}
