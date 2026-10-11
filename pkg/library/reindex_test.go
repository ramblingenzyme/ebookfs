package library

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// offLayouts are shapes a book directory can be found in that a restart moves
// back to its canonical location.
var offLayouts = []struct {
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
		name: "sort-name directory", title: "The Title", authors: []string{"Alice Smith"},
		legacyAuthorDir: "Smith, Alice", legacyEpub: "",
	},
	{
		// Rename preserves size and mtime, so only the filename shows it.
		name: "epub renamed by hand", title: "Renamed", authors: []string{"Alice"},
		legacyAuthorDir: "Alice", legacyEpub: "hand-renamed.epub",
	},
}

// Walk only checks that meta.toml and an *.epub exist, so neither needs valid
// contents.
func writeManualBookDir(t *testing.T, lib *Library, libraryPath string) {
	t.Helper()
	l := lib
	dir := filepath.Dir(l.store.AbsPath(filepath.Join(libraryPath, "meta.toml")))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "meta.toml"), []byte("id = 999\n"))
	writeFile(t, filepath.Join(dir, "book.epub"), []byte("manual"))
}

// stageLegacyLayout ingests a book, then moves its directory to
// legacyAuthorDir with the library closed, which may be where it already is. A
// non-empty legacyEpub also renames the epub inside it. It returns the book and the canonical location a reindex
// must restore.
func stageLegacyLayout(t *testing.T, cfg Config, title string, authors []string, legacyAuthorDir, legacyEpub string) (*Book, book.Location) {
	t.Helper()

	b := ingestStopped(t, cfg, title, authors...)
	canonical := book.Location{EpubPath: b.EpubPath()}

	legacyDir := filepath.Join(cfg.Root, legacyAuthorDir, fmt.Sprintf("%s (%d)", title, b.ID()))
	if canonicalDir := filepath.Join(cfg.Root, canonical.Dir()); canonicalDir != legacyDir {
		if err := os.MkdirAll(filepath.Dir(legacyDir), 0755); err != nil {
			t.Fatalf("mkdir legacy parent: %v", err)
		}
		if err := os.Rename(canonicalDir, legacyDir); err != nil {
			t.Fatalf("move book dir to %q: %v", legacyAuthorDir, err)
		}
	}
	if legacyEpub != "" {
		if err := os.Rename(filepath.Join(legacyDir, canonical.Filename()), filepath.Join(legacyDir, legacyEpub)); err != nil {
			t.Fatalf("rename epub to %q: %v", legacyEpub, err)
		}
	}
	return b, canonical
}

func TestNeedsReindexClosedIndex(t *testing.T) {
	lib := openTestLibrary(t)
	lib.index.Close()

	if !lib.needsReindex() {
		t.Error("needsReindex should return true when index check fails")
	}
}

func TestStoreDrifted(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, lib *Library, book *Book)
		want   bool
	}{
		{
			"nothing changed",
			func(*testing.T, *Library, *Book) {},
			false,
		},
		{
			"book directory added by hand",
			func(t *testing.T, lib *Library, _ *Book) {
				writeManualBookDir(t, lib, "Manual/Added Book (999)")
			},
			true,
		},
		{
			"book directory removed by hand",
			func(t *testing.T, lib *Library, book *Book) {
				dir := filepath.Join(lib.store.Root(), filepath.Dir(book.EpubPath()))
				if err := os.RemoveAll(dir); err != nil {
					t.Fatalf("remove book dir: %v", err)
				}
			},
			true,
		},
		{
			"epub swapped for a different one",
			func(t *testing.T, lib *Library, book *Book) {
				absEpub := lib.store.AbsPath(book.EpubPath())
				writeFile(t, absEpub, buildTestEpub(t, "A Completely Different And Much Longer Title"))
			},
			true,
		},
		{
			// Same byte count, different content, so only mtime separates them.
			// Pinned explicitly: a coarse clock reuses it within a tick.
			"epub swapped for one of the same size",
			func(t *testing.T, lib *Library, book *Book) {
				absEpub := lib.store.AbsPath(book.EpubPath())
				orig, err := os.ReadFile(absEpub)
				if err != nil {
					t.Fatalf("read epub: %v", err)
				}
				writeFile(t, absEpub, bytes.Repeat([]byte("X"), len(orig)))
				mt := book.DateModified().Add(-time.Hour)
				if err := os.Chtimes(absEpub, mt, mt); err != nil {
					t.Fatalf("chtimes: %v", err)
				}
			},
			true,
		},
		{
			// The opposite case, and why size is compared at all: mtime pinned
			// back, only the length gives the change away.
			"epub resized under an unchanged mtime",
			func(t *testing.T, lib *Library, book *Book) {
				absEpub := lib.store.AbsPath(book.EpubPath())
				fi, err := os.Stat(absEpub)
				if err != nil {
					t.Fatalf("stat epub: %v", err)
				}
				writeFile(t, absEpub, bytes.Repeat([]byte("X"), int(fi.Size())+512))
				if err := os.Chtimes(absEpub, fi.ModTime(), fi.ModTime()); err != nil {
					t.Fatalf("chtimes: %v", err)
				}
			},
			true,
		},
		{
			// Rename preserves size and mtime; only the filename catches it.
			"epub renamed in place",
			func(t *testing.T, lib *Library, book *Book) {
				absEpub := lib.store.AbsPath(book.EpubPath())
				renamed := filepath.Join(filepath.Dir(absEpub), "hand-renamed.epub")
				if err := os.Rename(absEpub, renamed); err != nil {
					t.Fatalf("rename epub: %v", err)
				}
			},
			true,
		},
		{
			"meta.toml edited by hand",
			func(t *testing.T, lib *Library, book *Book) {
				metaPath := metaPathOf(book, lib.store.Root())
				writeFile(t, metaPath, fmt.Appendf(nil, "id = %d\nstatus = \"read\"\n", book.ID()))
				// Coarse-clock filesystems reuse an mtime within a tick; pin it.
				mt := book.DateModified().Add(-time.Hour)
				if err := os.Chtimes(metaPath, mt, mt); err != nil {
					t.Fatalf("chtimes meta: %v", err)
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

func TestStoreCleanAfterEdit(t *testing.T) {
	// A meta-only edit skips the epub rewrite; a title edit rewrites the epub
	// and moves the directory. Both must leave the index clean.
	tests := []struct {
		name  string
		edits Edits
	}{
		{"meta only", Edits{Status: new(book.StatusRead)}},
		{"title change", Edits{Title: new("A Thoroughly Different Title")}},
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

// A manual edit made while down is picked up by a plain restart, no -reindex.
func TestOpenReindexesOnDrift(t *testing.T) {
	cfg := testConfig(t)

	book := ingestStopped(t, cfg, "Before")
	const want = "A Completely Different And Much Longer Title"
	writeFile(t, filepath.Join(cfg.Root, book.EpubPath()), buildTestEpub(t, want))

	got, err := openLib(t, cfg).Get(book.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title() != want {
		t.Errorf("Title = %q, want %q (drift not picked up on restart)", got.Title(), want)
	}
}

// A same-size sidecar edit with the old mtime restored is invisible to the
// drift check, so only WithForceReindex picks it up.
func TestForceReindexSeesUndetectedDrift(t *testing.T) {
	cfg := testConfig(t)

	book := ingestStopped(t, cfg, "Test")
	path := metaPathOf(book, cfg.Root)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	edited := bytes.Replace(data, []byte("rating = 0.0"), []byte("rating = 4.0"), 1)
	if bytes.Equal(edited, data) {
		t.Fatalf("meta.toml has no %q line:\n%s", "rating = 0.0", data)
	}
	writeFile(t, path, edited)
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	rating := func(opts ...Option) float64 {
		lib := openLib(t, cfg, opts...)
		defer lib.Close()
		got, err := lib.Get(book.ID())
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return got.Rating()
	}
	if got := rating(); got != 0 {
		t.Fatalf("plain restart saw the edit (Rating = %v), so this test no longer reaches the forced path", got)
	}
	if got := rating(WithForceReindex()); got != 4 {
		t.Errorf("Rating after a forced reindex = %v, want 4", got)
	}
}

// A plain restart, no -reindex. File state is recorded before the canonical
// move, and stays accurate after it because rename preserves size and mtime.
func TestRestartReconcilesTheDisk(t *testing.T) {
	for _, tc := range offLayouts {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			book, canonical := stageLegacyLayout(t, cfg, tc.title, tc.authors, tc.legacyAuthorDir, tc.legacyEpub)

			lib := openLib(t, cfg)

			if _, err := lib.Content(book.ID()); err != nil {
				t.Errorf("Content after restart: %v (index still points at a stale path)", err)
			}
			got, err := lib.Get(book.ID())
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.EpubPath() != canonical.EpubPath {
				t.Errorf("EpubPath = %q, want %q", got.EpubPath(), canonical.EpubPath)
			}
			if _, err := os.Stat(filepath.Join(cfg.Root, canonical.EpubPath)); err != nil {
				t.Errorf("canonical epub missing after restart: %v", err)
			}
			if drifted(t, lib) {
				t.Error("storeDrifted() = true after the restart, so every startup would reindex again")
			}
		})
	}
}

// An unstattable epub goes unindexed, but its readable sidecar still reserves
// the id. The index is dropped first, or the surviving sequence hands out ids
// and the reservation is never what keeps them apart.
func TestUnstattableBookReservesID(t *testing.T) {
	cfg := testConfig(t)
	book := ingestStopped(t, cfg, "Ghost")
	breakEpub(t, book, cfg.Root)
	dropIndex(t, cfg)

	next := ingestTestEpub(t, openLib(t, cfg), buildTestEpub(t, "Newcomer"))
	if next.ID() <= book.ID() {
		t.Errorf("new book got id %d, reusing unstattable book %d's id", next.ID(), book.ID())
	}
}

// Last resort for an unparseable sidecar: the id is still in the directory
// name. Reissuing it makes the repaired sidecar fatal (docs/DECISIONS.md #14).
func TestUnreadableMetaReservesIDFromPath(t *testing.T) {
	cfg := testConfig(t)
	book := ingestStopped(t, cfg, "Ghost")
	meta, err := os.ReadFile(metaPathOf(book, cfg.Root))
	if err != nil {
		t.Fatalf("read meta.toml: %v", err)
	}
	writeFile(t, metaPathOf(book, cfg.Root), []byte("id = \"not an integer\"\n"))
	dropIndex(t, cfg)

	lib2 := openLib(t, cfg)
	next := ingestTestEpub(t, lib2, buildTestEpub(t, "Newcomer"))
	if next.ID() <= book.ID() {
		t.Fatalf("new book got id %d, reusing id %d held by the book with the unreadable sidecar",
			next.ID(), book.ID())
	}
	if err := lib2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Repairing the sidecar must not resurrect a collision.
	writeFile(t, metaPathOf(book, cfg.Root), meta)
	lib3 := openLib(t, cfg)

	got, err := lib3.Search(Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d books, want both the repaired book and the newcomer indexed", len(got))
	}
}

// meta.toml is hand-editable, so a repeated tag can reach the rebuild without
// passing Validate. Inside the rebuild's one transaction, a constraint error
// fails every startup.
func TestRepeatedTagInMetaDoesNotFailOpen(t *testing.T) {
	cfg := testConfig(t)
	book := ingestStopped(t, cfg, "Echo")
	writeFile(t, metaPathOf(book, cfg.Root), fmt.Appendf(nil, "id = %d\ncustom_tags = [\"a\", \"b\", \"a\"]\n", book.ID()))

	got, err := openLib(t, cfg).Get(book.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if want := []string{"a", "b"}; !slices.Equal(got.Tags(), want) {
		t.Errorf("tags = %v, want %v", got.Tags(), want)
	}
}

// Two directories claiming one id is fatal (DECISIONS #14). The error names
// both, where the bare SQLite constraint error names neither.
func TestDuplicateBookIDFailsOpenNamingBothPaths(t *testing.T) {
	cfg := testConfig(t)
	book := ingestStopped(t, cfg, "Twin")

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
		writeFile(t, filepath.Join(dst, name), data)
	}

	lib2, err := Open(cfg)
	if err == nil {
		lib2.Close()
		t.Fatal("Open succeeded with two directories claiming one book id, want a fatal error")
	}
	for _, want := range []string{book.Dir(), filepath.Join("Copies", filepath.Base(src))} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Open error %q does not name the conflicting directory %q", err, want)
		}
	}
}

// A rebuild that skips a book records what it found, so the next start does
// not rebuild again. A repaired book changes that record and is indexed.
func TestBrokenBooksSettle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		breaks func(t *testing.T, cfg Config, b *Book)
		repair bool
	}{
		{"corrupt epub", func(t *testing.T, cfg Config, b *Book) {
			// store.Walk still sees a book: meta.toml is intact and an *.epub is
			// present.
			writeFile(t, filepath.Join(cfg.Root, b.EpubPath()), []byte("not a zip archive"))
		}, true},
		{"unstattable epub", func(t *testing.T, cfg Config, b *Book) {
			breakEpub(t, b, cfg.Root)
		}, true},
		// The directory still has to be recorded, or it looks like one that
		// appeared from nowhere.
		{"unstattable epub and unreadable meta", func(t *testing.T, cfg Config, b *Book) {
			breakEpub(t, b, cfg.Root)
			writeFile(t, metaPathOf(b, cfg.Root), []byte("id = \"not an integer\"\n"))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			b := ingestStopped(t, cfg, "Doomed")

			tc.breaks(t, cfg, b)
			assertSettlesClean(t, cfg)
			if !tc.repair {
				return
			}

			epub := filepath.Join(cfg.Root, b.EpubPath())
			if err := os.Remove(epub); err != nil {
				t.Fatalf("remove broken epub: %v", err)
			}
			writeFile(t, epub, buildTestEpub(t, "Repaired"))
			got, err := openLib(t, cfg).Search(Query{})
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != 1 || got[0].Title() != "Repaired" {
				t.Errorf("after repair got %d books (%v), want the repaired book indexed", len(got), got)
			}
		})
	}
}
