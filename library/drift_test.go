package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ptr matches the helper the model and epub test packages already use.
func ptr[T any](v T) *T { return &v }

// drifted reports storeDrifted's verdict, discarding the store scan it returns
// for the reindex path to reuse.
func drifted(t *testing.T, lib Library) bool {
	t.Helper()
	_, d := lib.(*libraryImpl).storeDrifted()
	return d
}

// writeManualBookDir lays down a book directory storeDrifted (via store.Walk)
// will discover, without going through the library's ingest path — simulating
// a book added directly to the store on disk. Walk only checks for meta.toml's
// presence and an *.epub file, so their contents don't need to be valid.
func writeManualBookDir(t *testing.T, lib Library, libraryPath string) {
	t.Helper()
	l := lib.(*libraryImpl)
	dir := filepath.Dir(l.store.AbsPath(libraryPath, "meta.toml"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte("id = 999\n"), 0644); err != nil {
		t.Fatalf("write meta.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "book.epub"), []byte("manual"), 0644); err != nil {
		t.Fatalf("write book.epub: %v", err)
	}
}

func TestStoreDriftedCleanIndex(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Clean"))

	if drifted(t, lib) {
		t.Error("storeDrifted() = true, want false for an index that matches the store")
	}
}

func TestStoreDriftedDetectsAddedBook(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

	writeManualBookDir(t, lib, "Manual/Added Book (999)")

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after a book directory was added by hand")
	}
}

func TestStoreDriftedDetectsRemovedBook(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Doomed"))

	if err := os.RemoveAll(filepath.Dir(book.EpubPath)); err != nil {
		t.Fatalf("remove book dir: %v", err)
	}

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after a book directory was removed by hand")
	}
}

func TestStoreDriftedDetectsSwappedEpub(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

	swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
	if err := os.WriteFile(book.EpubPath, swapped, 0644); err != nil {
		t.Fatalf("swap epub: %v", err)
	}

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after the epub file was swapped by hand")
	}
}

// TestStoreDriftedDetectsSameSizeSwap verifies that replacing an epub with
// different content of the exact same byte size is still detected as drift via
// mtime — something the old size-only check would miss.
func TestStoreDriftedDetectsSameSizeSwap(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

	// Read the original bytes to determine its exact size.
	orig, err := os.ReadFile(book.EpubPath)
	if err != nil {
		t.Fatalf("read epub: %v", err)
	}
	// Write a blob of exactly the same size but different content.
	padded := make([]byte, len(orig))
	for i := range padded {
		padded[i] = 'X'
	}
	if err := os.WriteFile(book.EpubPath, padded, 0644); err != nil {
		t.Fatalf("write same-size blob: %v", err)
	}
	// Set a deterministically different mtime so the test is not racy on
	// filesystems where a fast write completes within the same clock tick.
	mt := book.Meta.DateModified.Add(-time.Hour)
	if err := os.Chtimes(book.EpubPath, mt, mt); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after same-size epub swap")
	}
}

// TestStoreDriftedDetectsRenamedEpub verifies that renaming the epub within its
// book directory is drift. Rename preserves both size and mtime, so a check
// built only on those two is blind to it — and the index would go on serving an
// epub_filename that no longer exists, failing every read with ENOENT until
// someone forced a reindex by hand.
func TestStoreDriftedDetectsRenamedEpub(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Renamed"))

	dir := filepath.Dir(book.EpubPath)
	if err := os.Rename(book.EpubPath, filepath.Join(dir, "hand-renamed.epub")); err != nil {
		t.Fatalf("rename epub: %v", err)
	}

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after the epub was renamed in place")
	}
}

// TestRenamedEpubHealedOnRestart is the end-to-end half: the drift above must
// be repaired by a plain restart, which reindexes and — via the canonical
// Layout/Move pass — puts the epub back under its canonical name.
func TestRenamedEpubHealedOnRestart(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Renamed"))
	canonical := book.Location.EpubFilename
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Dir(book.EpubPath)
	if err := os.Rename(book.EpubPath, filepath.Join(dir, "hand-renamed.epub")); err != nil {
		t.Fatalf("rename epub: %v", err)
	}

	lib2 := openLib(t, cfg, false) // plain restart, no -reindex

	if _, err := lib2.OpenEpub(book.Meta.ID); err != nil {
		t.Errorf("OpenEpub after restart: %v (index still points at a stale filename)", err)
	}
	got, err := lib2.Query(model.Filter{ID: book.Meta.ID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].EpubFilename != canonical {
		t.Errorf("EpubFilename = %v, want the canonical %q restored", got, canonical)
	}
	if drifted(t, lib2) {
		t.Error("storeDrifted() = true after the healing reindex, so every startup would reindex again")
	}
}

// TestUnstattableBookReservesID covers the id-reservation half of the reindex
// worker: a book whose epub cannot be stat'd (here a dangling symlink, which
// store.Walk still reports because findEpub only reads the directory entry) is
// left unindexed, but its meta.toml is readable so its id is taken and must not
// be handed to a later ingest.
func TestUnstattableBookReservesID(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Ghost"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.Remove(book.EpubPath); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(book.EpubPath), "nowhere.epub")
	if err := os.Symlink(dangling, book.EpubPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	lib2 := openLib(t, cfg, false)

	next := ingestTestEpub(t, lib2, buildTestEpub(t, "Newcomer"))
	if next.Meta.ID <= book.Meta.ID {
		t.Errorf("new book got id %d, reusing unstattable book %d's id", next.Meta.ID, book.Meta.ID)
	}
}

// TestUnstattableBookSettlesClean is the other half of TestUnstattableBookReservesID:
// a book whose files can't be stat'd must stop being drift after one rebuild
// has recorded it as unobserved. Otherwise storeDrifted reports true on every
// startup and the whole library is reindexed forever over one broken file.
func TestUnstattableBookSettlesClean(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Ghost"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A dangling symlink: store.Walk still reports the directory (findEpub only
	// reads the directory entry) but os.Stat follows the link and fails.
	if err := os.Remove(book.EpubPath); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(book.EpubPath), "nowhere.epub")
	if err := os.Symlink(dangling, book.EpubPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// First restart reindexes and records the unobserved marker; from then on
	// every further plain restart must see a clean index.
	for i := 1; i <= 3; i++ {
		l, err := Open(cfg, false)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		d := drifted(t, l)
		if err := l.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
		if d {
			t.Fatalf("storeDrifted() = true on restart %d — one unreadable book forces a full reindex on every startup", i)
		}
	}

	// Repairing it must still be noticed: real file state differs from the
	// unobserved marker, so the book earns another indexing attempt.
	if err := os.Remove(book.EpubPath); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.WriteFile(book.EpubPath, buildTestEpub(t, "Ghost"), 0644); err != nil {
		t.Fatalf("repair epub: %v", err)
	}
	lib2 := openLib(t, cfg, false)

	got, err := lib2.Query(model.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("after repair got %d books, want the repaired book indexed", len(got))
	}
}

// TestUnreadableMetaAndUnstattableEpubSettlesClean covers both halves of a book
// failing at once: the epub can't be stat'd and the sidecar can't be parsed. The
// rebuild has no trustworthy file state for it, but it must still record the
// directory — one left out of both books and skipped_books reads as a path that
// appeared on disk unaccounted for, so storeDrifted reports drift and the whole
// library is reindexed on every startup.
func TestUnreadableMetaAndUnstattableEpubSettlesClean(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Ghost"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Dangling symlink, so store.Walk still reports the directory but os.Stat
	// follows the link and fails.
	if err := os.Remove(book.EpubPath); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(book.EpubPath), "nowhere.epub")
	if err := os.Symlink(dangling, book.EpubPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// Present but unparseable, so Walk still sees a book directory while
	// ReadMeta fails.
	metaPath := filepath.Join(filepath.Dir(book.EpubPath), "meta.toml")
	if err := os.WriteFile(metaPath, []byte("id = \"not an integer\"\n"), 0644); err != nil {
		t.Fatalf("corrupt meta.toml: %v", err)
	}

	// First restart reindexes and records the directory; every further plain
	// restart must then see a clean index.
	for i := 1; i <= 3; i++ {
		l, err := Open(cfg, false)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		d := drifted(t, l)
		if err := l.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
		if d {
			t.Fatalf("storeDrifted() = true on restart %d — a book with an unreadable sidecar "+
				"and an unstattable epub forces a full reindex on every startup", i)
		}
	}
}

// TestDuplicateBookIDFailsOpenNamingBothPaths covers a book directory copied on
// disk, giving two directories the same meta.toml id. This is fatal by design
// (DECISIONS.md #14) — the test pins that, and that the error names both
// offending directories, since the bare SQLite constraint error ("UNIQUE
// constraint failed: books.id") leaves the user with nothing to act on.
func TestDuplicateBookIDFailsOpenNamingBothPaths(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Twin"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Copy the whole book directory, meta.toml and all, to a second path.
	src := filepath.Dir(book.EpubPath)
	dst := filepath.Join(cfg.Root, "Copies", filepath.Base(src))
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("mkdir copy: %v", err)
	}
	for _, name := range []string{"meta.toml", book.Location.EpubFilename} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	lib2, err := Open(cfg, false)
	if err == nil {
		lib2.Close()
		t.Fatal("Open succeeded with two directories claiming one book id, want a fatal error")
	}
	// Both directories must appear, or the user has no way to know which to fix.
	for _, want := range []string{book.Location.LibraryPath, filepath.Join("Copies", filepath.Base(src))} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Open error %q does not name the conflicting directory %q", err, want)
		}
	}
}

// TestStoreDriftedDetectsManualMetaEdit verifies that editing meta.toml
// directly on disk (bypassing the library) is detected as drift. The old
// size-only check would miss this entirely since it never looked at meta.toml.
func TestStoreDriftedDetectsManualMetaEdit(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Book"))

	metaPath := lib.(*libraryImpl).store.MetaPath(book.Location)
	// Write a modified meta.toml to simulate hand-editing the sidecar.
	if err := os.WriteFile(metaPath, []byte("id = 1\nstatus = \"read\"\n"), 0644); err != nil {
		t.Fatalf("write meta.toml: %v", err)
	}
	// Ensure a deterministically different mtime for the same reason as the
	// epub swap test: fast writes may not advance the clock tick.
	mt := book.Meta.DateModified.Add(-time.Hour)
	if err := os.Chtimes(metaPath, mt, mt); err != nil {
		t.Fatalf("chtimes meta: %v", err)
	}

	if !drifted(t, lib) {
		t.Error("storeDrifted() = false, want true after meta.toml was hand-edited")
	}
}

// TestStoreCleanAfterEdit covers the other direction from the drift tests
// above: a change made *through* the library must leave the index agreeing
// with the store. Edit rewrites the epub, may move the book directory, and
// always rewrites meta.toml — if it failed to re-record the file state after
// those writes, every subsequent startup would reindex the whole library.
func TestStoreCleanAfterEdit(t *testing.T) {
	// A meta-only edit skips the epub rewrite; a title edit rewrites the epub
	// and moves the directory. Both must leave the index clean.
	tests := []struct {
		name  string
		edits model.Edits
	}{
		{"meta only", model.Edits{Status: ptr(string(model.StatusRead))}},
		{"title change", model.Edits{Title: ptr("A Thoroughly Different Title")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lib := openTestLibrary(t)
			book := ingestTestEpub(t, lib, buildTestEpub(t, "Before"))

			if _, err := lib.Edit(book.Meta.ID, tc.edits); err != nil {
				t.Fatalf("Edit: %v", err)
			}
			if drifted(t, lib) {
				t.Error("storeDrifted() = true after Edit, want false — a library-mediated change left stale file state in the index, so every startup will reindex")
			}
		})
	}
}

// TestCorruptEpubDoesNotReindexForever covers the pathology that a directory
// the rebuild cannot index used to cause: it produced no books row, so drift
// detection saw an unexplained directory and rebuilt on every single startup.
// The rebuild now records skipped paths, so a second plain restart stays clean.
func TestCorruptEpubDoesNotReindexForever(t *testing.T) {
	cfg := testConfig(t)

	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Doomed"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Corrupt the epub so it can no longer be parsed. The directory still looks
	// like a book to store.Walk: meta.toml is intact and an *.epub is present.
	if err := os.WriteFile(book.EpubPath, []byte("not a zip archive"), 0644); err != nil {
		t.Fatalf("corrupt epub: %v", err)
	}

	// First restart: genuine drift, so this one reindexes and records the skip.
	lib2 := openLib(t, cfg, false)
	if drifted(t, lib2) {
		t.Error("storeDrifted() = true right after a reindex that skipped the corrupt book — the skip was not recorded, so startups will reindex forever")
	}
	if err := lib2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Repairing the book must still be noticed: its file state changes, so the
	// recorded skip no longer matches and the book earns another attempt.
	if err := os.WriteFile(book.EpubPath, buildTestEpub(t, "Repaired"), 0644); err != nil {
		t.Fatalf("repair epub: %v", err)
	}
	lib3 := openLib(t, cfg, false)

	got, err := lib3.Query(model.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Repaired" {
		t.Errorf("after repair got %d books (%v), want the repaired book indexed", len(got), got)
	}
}

// TestOpenReindexesOnDrift is the end-to-end case: a manual edit to the store
// while the server is down must be picked up on the next plain restart (no
// -reindex flag), because Open consults storeDrifted alongside needsReindex.
func TestOpenReindexesOnDrift(t *testing.T) {
	cfg := testConfig(t)

	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Before"))
	id := book.Meta.ID
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
	if err := os.WriteFile(book.EpubPath, swapped, 0644); err != nil {
		t.Fatalf("swap epub while server is down: %v", err)
	}

	lib2 := openLib(t, cfg, false) // plain restart, no -reindex

	got, err := lib2.Query(model.Filter{ID: id})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := "A Completely Different And Much Longer Title"
	if got[0].Title != want {
		t.Errorf("Title = %q, want %q (drift not picked up on restart)", got[0].Title, want)
	}
}

// TestReindexMigratesToCanonicalPath verifies that the Layout/Move pass during
// reindex relocates books from old-style paths (single-author directories,
// sort-name directories) to the canonical naming convention. The test ingests
// a multi-author book, moves it to a single-author directory on disk, then
// triggers a reindex and confirms the book is restored to the "Alice & Bob"
// directory with the all-author epub filename.
func TestReindexMigratesToCanonicalPath(t *testing.T) {
	cfg := testConfig(t)
	root := cfg.Root

	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Test Title", "Alice", "Bob"))
	canonicalPath := book.Location.LibraryPath  // e.g. "Alice & Bob/Test Title (1)"
	canonicalEpub := book.Location.EpubFilename // e.g. "Test Title - Alice & Bob.epub"
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Move the book directory from the multi-author path to a single-author
	// path on disk, simulating an old-style layout or external interference.
	oldDir := filepath.Join(root, "Alice", fmt.Sprintf("Test Title (%d)", book.Meta.ID))
	if err := os.MkdirAll(filepath.Dir(oldDir), 0755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, canonicalPath), oldDir); err != nil {
		t.Fatalf("rename book dir to old path: %v", err)
	}
	// The epub filename also needs the old single-author form.
	oldEpub := "Test Title - Alice.epub"
	if err := os.Rename(filepath.Join(oldDir, canonicalEpub), filepath.Join(oldDir, oldEpub)); err != nil {
		t.Fatalf("rename epub to old filename: %v", err)
	}

	// Reopen with force reindex — triggers Layout/Move.
	lib2 := openLib(t, cfg, true)

	got, err := lib2.Query(model.Filter{ID: book.Meta.ID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1", len(got))
	}
	if got[0].Location.LibraryPath != canonicalPath {
		t.Errorf("LibraryPath = %q, want %q", got[0].Location.LibraryPath, canonicalPath)
	}
	if got[0].Location.EpubFilename != canonicalEpub {
		t.Errorf("EpubFilename = %q, want %q", got[0].Location.EpubFilename, canonicalEpub)
	}

	// The canonical directory should exist on disk.
	if _, err := os.Stat(filepath.Join(root, canonicalPath)); err != nil {
		t.Errorf("canonical dir missing after reindex: %v", err)
	}
}

// TestReindexLeavesIndexClean is the closing half of drift detection: after a
// rebuild the index must agree with the store, or every subsequent startup
// reindexes again. The canonical-move case is the one that can silently break
// it — reindex records each book's file state before the Layout/Move pass (so
// its reuse of storeDrifted's scan keys correctly), and that recorded state is
// only still accurate afterwards because rename preserves size and mtime.
func TestReindexLeavesIndexClean(t *testing.T) {
	cfg := testConfig(t)
	root := cfg.Root

	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Test Title", "Alice", "Bob"))
	canonicalPath := book.Location.LibraryPath
	canonicalEpub := book.Location.EpubFilename
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Put the book at an old-style single-author path so the next Open drifts
	// and its reindex has canonical moves to perform.
	oldDir := filepath.Join(root, "Alice", fmt.Sprintf("Test Title (%d)", book.Meta.ID))
	if err := os.MkdirAll(filepath.Dir(oldDir), 0755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, canonicalPath), oldDir); err != nil {
		t.Fatalf("rename book dir to old path: %v", err)
	}
	if err := os.Rename(filepath.Join(oldDir, canonicalEpub), filepath.Join(oldDir, "Test Title - Alice.epub")); err != nil {
		t.Fatalf("rename epub to old filename: %v", err)
	}

	// Plain restart: storeDrifted must notice, and its reindex must migrate.
	lib2 := openLib(t, cfg, false)

	if drifted(t, lib2) {
		t.Error("storeDrifted() = true after a reindex, want false — the rebuild left the index disagreeing with the store, so every startup will reindex again")
	}
}

// TestReindexMigratesFromSortNamePath verifies that a book originally placed
// at a SortName-based path (e.g. "Smith, Alice/Title (id)/") is relocated to
// the display-name path ("Alice/Title (id)/") during reindex. The epub filename
// is unaffected — it always used display name, never SortName.
func TestReindexMigratesFromSortNamePath(t *testing.T) {
	cfg := testConfig(t)
	root := cfg.Root

	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "The Title", "Alice"))
	canonicalPath := book.Location.LibraryPath  // "Alice/The Title (1)"
	canonicalEpub := book.Location.EpubFilename // "The Title - Alice.epub"
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Rename directory to old SortName-based path (Last, First).
	oldDir := filepath.Join(root, "Smith, Alice", fmt.Sprintf("The Title (%d)", book.Meta.ID))
	if err := os.MkdirAll(filepath.Dir(oldDir), 0755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, canonicalPath), oldDir); err != nil {
		t.Fatalf("rename to SortName path: %v", err)
	}
	// Epub filename stays the same (it always used display Name, not SortName).

	lib2 := openLib(t, cfg, true)

	got, err := lib2.Query(model.Filter{ID: book.Meta.ID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1", len(got))
	}
	if got[0].Location.LibraryPath != canonicalPath {
		t.Errorf("LibraryPath = %q, want %q", got[0].Location.LibraryPath, canonicalPath)
	}
	if got[0].Location.EpubFilename != canonicalEpub {
		t.Errorf("EpubFilename = %q, want %q", got[0].Location.EpubFilename, canonicalEpub)
	}
	if _, err := os.Stat(filepath.Join(root, canonicalPath)); err != nil {
		t.Errorf("canonical dir missing after reindex: %v", err)
	}
}
