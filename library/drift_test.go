package library

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// writeManualBookDir lays down a book directory storeDrifted (via store.Walk)
// will discover, without going through the library's ingest path — simulating
// a book added directly to the store on disk. Walk only checks for meta.toml's
// presence and an *.epub file, so their contents don't need to be valid.
func writeManualBookDir(t *testing.T, lib Library, libraryPath string) {
	t.Helper()
	l := lib.(*libraryImpl)
	dir := filepath.Dir(l.store.AbsPath(filepath.Join(libraryPath, "meta.toml")))
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

// TestStoreDrifted covers the verdict itself: what counts as the store having
// moved out from under the index. Every case ingests one book through the
// library, changes the store behind its back, and asks whether the change is
// noticed — so a comparison that goes blind to any one of them surfaces here,
// rather than as startups that quietly stop reindexing.
func TestStoreDrifted(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, lib Library, book *Book)
		want   bool
	}{
		{
			"nothing changed",
			func(*testing.T, Library, *Book) {},
			false,
		},
		{
			"book directory added by hand",
			func(t *testing.T, lib Library, _ *Book) {
				writeManualBookDir(t, lib, "Manual/Added Book (999)")
			},
			true,
		},
		{
			"book directory removed by hand",
			func(t *testing.T, lib Library, book *Book) {
				dir := filepath.Join(lib.(*libraryImpl).store.Root(), filepath.Dir(book.EpubPath()))
				if err := os.RemoveAll(dir); err != nil {
					t.Fatalf("remove book dir: %v", err)
				}
			},
			true,
		},
		{
			"epub swapped for a different one",
			func(t *testing.T, lib Library, book *Book) {
				absEpub := lib.(*libraryImpl).store.AbsPath(book.EpubPath())
				swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
				if err := os.WriteFile(absEpub, swapped, 0644); err != nil {
					t.Fatalf("swap epub: %v", err)
				}
			},
			true,
		},
		{
			// The case a size-only check misses: same byte count, different
			// content, so only mtime separates them. The mtime is set
			// explicitly because a fast write can land in the same clock tick
			// as the recorded one on a coarse-clock filesystem.
			"epub swapped for one of the same size",
			func(t *testing.T, lib Library, book *Book) {
				absEpub := lib.(*libraryImpl).store.AbsPath(book.EpubPath())
				orig, err := os.ReadFile(absEpub)
				if err != nil {
					t.Fatalf("read epub: %v", err)
				}
				if err := os.WriteFile(absEpub, bytes.Repeat([]byte("X"), len(orig)), 0644); err != nil {
					t.Fatalf("write same-size blob: %v", err)
				}
				mt := book.DateModified().Add(-time.Hour)
				if err := os.Chtimes(absEpub, mt, mt); err != nil {
					t.Fatalf("chtimes: %v", err)
				}
			},
			true,
		},
		{
			// The mirror of the case above, and the reason size is compared at
			// all: a coarse-clock filesystem hands out the same mtime for two
			// writes in one tick, so here the mtime is pinned back to its
			// recorded value and only the length gives the change away.
			"epub resized under an unchanged mtime",
			func(t *testing.T, lib Library, book *Book) {
				absEpub := lib.(*libraryImpl).store.AbsPath(book.EpubPath())
				fi, err := os.Stat(absEpub)
				if err != nil {
					t.Fatalf("stat epub: %v", err)
				}
				grown := bytes.Repeat([]byte("X"), int(fi.Size())+512)
				if err := os.WriteFile(absEpub, grown, 0644); err != nil {
					t.Fatalf("write longer blob: %v", err)
				}
				if err := os.Chtimes(absEpub, fi.ModTime(), fi.ModTime()); err != nil {
					t.Fatalf("chtimes: %v", err)
				}
			},
			true,
		},
		{
			// Rename preserves size and mtime, so only the filename comparison
			// catches this. Miss it and the index goes on serving a path that
			// no longer exists, failing every read with ENOENT until someone
			// forces a reindex by hand.
			"epub renamed in place",
			func(t *testing.T, lib Library, book *Book) {
				absEpub := lib.(*libraryImpl).store.AbsPath(book.EpubPath())
				renamed := filepath.Join(filepath.Dir(absEpub), "hand-renamed.epub")
				if err := os.Rename(absEpub, renamed); err != nil {
					t.Fatalf("rename epub: %v", err)
				}
			},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lib := openTestLibrary(t)
			book := ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

			tc.change(t, lib, book)

			if got := drifted(t, lib); got != tc.want {
				t.Errorf("storeDrifted() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRenamedEpubHealedOnRestart is the end-to-end half: the drift above must
// be repaired by a plain restart, which reindexes and — via the canonical
// Layout/Move pass — puts the epub back under its canonical name.
func TestRenamedEpubHealedOnRestart(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Renamed"))
	canonical := book.Filename()
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Dir(filepath.Join(cfg.Root, book.EpubPath()))
	if err := os.Rename(filepath.Join(cfg.Root, book.EpubPath()), filepath.Join(dir, "hand-renamed.epub")); err != nil {
		t.Fatalf("rename epub: %v", err)
	}

	lib2 := openLib(t, cfg, false) // plain restart, no -reindex

	if _, err := lib2.Content(book.ID()); err != nil {
		t.Errorf("Content after restart: %v (index still points at a stale filename)", err)
	}
	got, err := lib2.Search(model.Query{IDs: []int64{book.ID()}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].Filename() != canonical {
		t.Errorf("EpubFilename = %v, want the canonical %q restored", got, canonical)
	}
	if drifted(t, lib2) {
		t.Error("storeDrifted() = true after the healing reindex, so every startup would reindex again")
	}
}

// TestUnstattableBookReservesID covers the id-reservation half of the reindex
// worker: a book whose epub cannot be stat'd is left unindexed, but its
// meta.toml is readable so its id is taken and must not be handed to a later
// ingest. The index is dropped first for the same reason as
// TestUnreadableMetaReservesIDFromPath — with the sequence still on disk the
// reservation is never what keeps the ids apart, and the test cannot fail.
func TestUnstattableBookReservesID(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Ghost"))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	breakEpub(t, book, cfg.Root)
	dropIndex(t, cfg)

	lib2 := openLib(t, cfg, false)

	next := ingestTestEpub(t, lib2, buildTestEpub(t, "Newcomer"))
	if next.ID() <= book.ID() {
		t.Errorf("new book got id %d, reusing unstattable book %d's id", next.ID(), book.ID())
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

	breakEpub(t, book, cfg.Root)
	assertSettlesClean(t, cfg)

	// Repairing it must still be noticed: real file state differs from the
	// unobserved marker, so the book earns another indexing attempt.
	if err := os.Remove(filepath.Join(cfg.Root, book.EpubPath())); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Root, book.EpubPath()), buildTestEpub(t, "Ghost"), 0644); err != nil {
		t.Fatalf("repair epub: %v", err)
	}
	lib2 := openLib(t, cfg, false)

	got, err := lib2.Search(model.Query{})
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

	breakEpub(t, book, cfg.Root)
	// Present but unparseable, so Walk still sees a book directory while
	// ReadMeta fails.
	if err := os.WriteFile(metaPathOf(book, cfg.Root), []byte("id = \"not an integer\"\n"), 0644); err != nil {
		t.Fatalf("corrupt meta.toml: %v", err)
	}

	assertSettlesClean(t, cfg)
}

// TestUnreadableMetaReservesIDFromPath covers the reindex worker's last resort
// for a book whose sidecar can't be parsed: the id is still legible in the
// directory name, so it is reserved from there. Without it the next ingest
// reissues that id, and the moment the sidecar is repaired the two directories
// claim one id and startup is fatal by design (DECISIONS.md #14) — a corrupt
// file turning into an unbootable library.
//
// The index is dropped so the rebuild starts with no id sequence and has only
// the store to learn from, which is the situation the fallback is for. Only the
// sidecar is broken; the epub stays parseable, so the read-meta branch is the
// only one that fires.
func TestUnreadableMetaReservesIDFromPath(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg, false)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Ghost"))
	meta, err := os.ReadFile(metaPathOf(book, cfg.Root))
	if err != nil {
		t.Fatalf("read meta.toml: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.WriteFile(metaPathOf(book, cfg.Root), []byte("id = \"not an integer\"\n"), 0644); err != nil {
		t.Fatalf("corrupt meta.toml: %v", err)
	}
	dropIndex(t, cfg)

	lib2 := openLib(t, cfg, false)
	next := ingestTestEpub(t, lib2, buildTestEpub(t, "Newcomer"))
	if next.ID() <= book.ID() {
		t.Fatalf("new book got id %d, reusing id %d held by the book with the unreadable sidecar",
			next.ID(), book.ID())
	}
	if err := lib2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Repairing the sidecar must not resurrect a collision.
	if err := os.WriteFile(metaPathOf(book, cfg.Root), meta, 0644); err != nil {
		t.Fatalf("repair meta.toml: %v", err)
	}
	lib3 := openLib(t, cfg, false)

	got, err := lib3.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d books, want both the repaired book and the newcomer indexed", len(got))
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
	src := filepath.Join(cfg.Root, filepath.Dir(book.EpubPath()))
	dst := filepath.Join(cfg.Root, "Copies", filepath.Base(src))
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("mkdir copy: %v", err)
	}
	for _, name := range []string{"meta.toml", book.Filename()} {
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
	for _, want := range []string{book.Dir(), filepath.Join("Copies", filepath.Base(src))} {
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

	metaPath := metaPathOf(book, lib.(*libraryImpl).store.Root())
	// Write a modified meta.toml to simulate hand-editing the sidecar.
	edited := fmt.Sprintf("id = %d\nstatus = \"read\"\n", book.ID())
	if err := os.WriteFile(metaPath, []byte(edited), 0644); err != nil {
		t.Fatalf("write meta.toml: %v", err)
	}
	// Ensure a deterministically different mtime for the same reason as the
	// epub swap test: fast writes may not advance the clock tick.
	mt := book.DateModified().Add(-time.Hour)
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
		{"meta only", model.Edits{Status: new(string(model.StatusRead))}},
		{"title change", model.Edits{Title: new("A Thoroughly Different Title")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lib := openTestLibrary(t)
			book := ingestTestEpub(t, lib, buildTestEpub(t, "Before"))

			if _, err := lib.Edit(book.ID(), tc.edits); err != nil {
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
	if err := os.WriteFile(filepath.Join(cfg.Root, book.EpubPath()), []byte("not a zip archive"), 0644); err != nil {
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
	if err := os.WriteFile(filepath.Join(cfg.Root, book.EpubPath()), buildTestEpub(t, "Repaired"), 0644); err != nil {
		t.Fatalf("repair epub: %v", err)
	}
	lib3 := openLib(t, cfg, false)

	got, err := lib3.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 || got[0].Title() != "Repaired" {
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
	id := book.ID()
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
	if err := os.WriteFile(filepath.Join(cfg.Root, book.EpubPath()), swapped, 0644); err != nil {
		t.Fatalf("swap epub while server is down: %v", err)
	}

	lib2 := openLib(t, cfg, false) // plain restart, no -reindex

	got, err := lib2.Search(model.Query{IDs: []int64{id}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := "A Completely Different And Much Longer Title"
	if got[0].Title() != want {
		t.Errorf("Title = %q, want %q (drift not picked up on restart)", got[0].Title(), want)
	}
}

// stageLegacyLayout ingests a book, closes the library, and then moves its
// directory to legacyAuthorDir — the state an upgraded library finds on disk,
// left by a naming convention it no longer uses. legacyEpub, when non-empty,
// also renames the epub inside it. It returns the book and the canonical
// location the next reindex has to restore.
func stageLegacyLayout(t *testing.T, cfg config.LibraryConfig, title string, authors []string, legacyAuthorDir, legacyEpub string) (*Book, book.Location) {
	t.Helper()

	lib := openLib(t, cfg, false)
	b := ingestTestEpub(t, lib, buildTestEpub(t, title, authors...))
	canonical := book.Unwrap(b).Location
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	legacyDir := filepath.Join(cfg.Root, legacyAuthorDir, fmt.Sprintf("%s (%d)", title, b.ID()))
	if err := os.MkdirAll(filepath.Dir(legacyDir), 0755); err != nil {
		t.Fatalf("mkdir legacy parent: %v", err)
	}
	if err := os.Rename(filepath.Join(cfg.Root, canonical.Dir()), legacyDir); err != nil {
		t.Fatalf("move book dir to %q: %v", legacyAuthorDir, err)
	}
	if legacyEpub != "" {
		if err := os.Rename(filepath.Join(legacyDir, canonical.Filename()), filepath.Join(legacyDir, legacyEpub)); err != nil {
			t.Fatalf("rename epub to %q: %v", legacyEpub, err)
		}
	}
	return b, canonical
}

// legacyLayouts are the pre-canonical shapes a book directory can be found in.
var legacyLayouts = []struct {
	name            string
	title           string
	authors         []string
	legacyAuthorDir string
	legacyEpub      string
}{
	{
		// Co-authored books were filed under their first author alone, and the
		// epub filename named only that author too.
		name: "single-author directory", title: "Test Title", authors: []string{"Alice", "Bob"},
		legacyAuthorDir: "Alice", legacyEpub: "Test Title - Alice.epub",
	},
	{
		// Author directories used the sort name. The epub filename always used
		// the display name, so it needs no rename here.
		name: "sort-name directory", title: "The Title", authors: []string{"Alice"},
		legacyAuthorDir: "Smith, Alice", legacyEpub: "",
	},
}

// TestReindexMigratesToCanonicalPath verifies the Layout/Move pass relocates a
// book from each old-style path to the canonical one, and that the index
// records where it ended up rather than where it was found.
func TestReindexMigratesToCanonicalPath(t *testing.T) {
	for _, tc := range legacyLayouts {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			book, canonical := stageLegacyLayout(t, cfg, tc.title, tc.authors, tc.legacyAuthorDir, tc.legacyEpub)

			lib := openLib(t, cfg, true)

			got, err := lib.Search(model.Query{IDs: []int64{book.ID()}})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d books, want 1", len(got))
			}
			if got[0].Dir() != canonical.Dir() {
				t.Errorf("Dir = %q, want %q", got[0].Dir(), canonical.Dir())
			}
			if got[0].Filename() != canonical.Filename() {
				t.Errorf("Filename = %q, want %q", got[0].Filename(), canonical.Filename())
			}
			if _, err := os.Stat(filepath.Join(cfg.Root, canonical.Dir())); err != nil {
				t.Errorf("canonical dir missing after reindex: %v", err)
			}
		})
	}
}

// TestReindexLeavesIndexClean is the closing half of drift detection: after a
// rebuild the index must agree with the store, or every subsequent startup
// reindexes again. The canonical-move case is the one that can silently break
// it — reindex records each book's file state before the Layout/Move pass (so
// its reuse of storeDrifted's scan keys correctly), and that recorded state is
// only still accurate afterwards because rename preserves size and mtime.
//
// It runs on a plain restart, not a forced one, so the drift verdict and the
// rebuild it triggers are both under test.
func TestReindexLeavesIndexClean(t *testing.T) {
	for _, tc := range legacyLayouts {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			stageLegacyLayout(t, cfg, tc.title, tc.authors, tc.legacyAuthorDir, tc.legacyEpub)

			lib := openLib(t, cfg, false)

			if drifted(t, lib) {
				t.Error("storeDrifted() = true after a reindex, want false — the rebuild left the index disagreeing with the store, so every startup will reindex again")
			}
		})
	}
}
