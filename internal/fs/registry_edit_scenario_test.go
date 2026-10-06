// Registry, views, and per-book directories composed together: public library
// edits and 9P field writes must rehome books through the registered hook.
package fs

import (
	"testing"

	go9pfs "github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestRegistryEditTitleRehomesInAllViews(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	lib.AddHook(reg)
	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	byID := views.NewByIDDir(reg)
	ingestRegistryScenarioBook(t, lib, "Old Title", "Alice")

	writeField(t, fstest.ChildAs[go9pfs.Dir](t, allBooks, "Old Title"), "title", "New Title")

	fstest.HasChild(t, allBooks, "New Title")
	fstest.NoChild(t, allBooks, "Old Title")
	fstest.HasChild(t, byID, "1. New Title")
	fstest.HasChild(t, fstest.ChildAs[go9pfs.Dir](t, byAuthor, "Alice"), "New Title")
}

func TestRegistryEditAuthorsRehomesInByAuthor(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	lib.AddHook(reg)
	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	ingestRegistryScenarioBook(t, lib, "Test", "Alice")

	writeField(t, fstest.ChildAs[go9pfs.Dir](t, allBooks, "Test"), "authors", "Bob")

	fstest.HasChild(t, fstest.ChildAs[go9pfs.Dir](t, byAuthor, "Bob"), "Test")
	fstest.NoChild(t, byAuthor, "Alice")
}

func TestRegistryEditStatusChangesReaderView(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	lib.AddHook(reg)
	allBooks := views.NewAllBooksDir(reg)
	readerDir := views.NewReaderDir(reg, mock.Exporter{StatusList: []string{"reading"}})
	book := ingestRegistryScenarioBook(t, lib, "Test", "Author1")

	fstest.ChildCount(t, readerDir, 0)
	writeField(t, fstest.ChildAs[go9pfs.Dir](t, allBooks, book.Title()), "status", "reading")
	fstest.HasChild(t, fstest.ChildAs[go9pfs.ModDir](t, readerDir, "Author1"), book.Filename())
}

// Otrunc followed by close is how a 9P client commits a field edit.
func writeField(t *testing.T, dir go9pfs.Dir, name, value string) {
	t.Helper()
	fstest.Fid(t, fstest.ChildAs[go9pfs.File](t, dir, name), 1).Set(proto.Otrunc, value)
}

