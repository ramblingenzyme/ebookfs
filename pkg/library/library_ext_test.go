package library_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Shutdown and t.Cleanup both close, so a second close must succeed.
func TestCloseMultiple(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Search returns the path relative to the store root, and every consumer joins
// it to the root. An empty or absolute path would leave the result unopenable.
func TestSearchReturnsARootRelativeEpubPath(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	want := ingestTestEpub(t, lib, buildTestEpub(t, "Findable", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Unrelated", "Bob"))

	got, err := lib.Search(library.Query{Titles: []string{"Findable"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search returned %d books, want just the matching one", len(got))
	}
	if got[0].ID() != want.ID() {
		t.Errorf("id = %d, want %d", got[0].ID(), want.ID())
	}
	if got[0].EpubPath() == "" {
		t.Fatal("EpubPath is empty — the result carries no path, so nothing can open it")
	}
	if filepath.IsAbs(got[0].EpubPath()) {
		t.Errorf("EpubPath = %q, want a relative path", got[0].EpubPath())
	}
	absPath := filepath.Join(cfg.Root, got[0].EpubPath())
	if _, err := os.Stat(absPath); err != nil {
		t.Errorf("EpubPath %q does not resolve: %v", got[0].EpubPath(), err)
	}
}

// Regression: a book filed before FAT sanitization has a ':' in its epub name,
// under a directory Layout still produces. A status-only edit must rename it in
// place, not fail on "destination already exists".
func TestEditSelfHealsLegacyUnsanitizedFilename(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	title := "Some Title: With Colon"
	book := ingestTestEpub(t, lib, buildTestEpub(t, title))
	id := book.ID()

	// Rename the on-disk epub to a name Layout would no longer produce, then
	// reindex so the index records the manual rename.
	root := cfg.Root
	dir := filepath.Join(root, filepath.Dir(book.EpubPath()))
	absEpub := filepath.Join(root, book.EpubPath())
	legacyName := "Some Title: With Colon - Alice.epub"
	if err := os.Rename(absEpub, filepath.Join(dir, legacyName)); err != nil {
		t.Fatal(err)
	}
	if err := lib.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// An unrelated field, so Layout keeps the directory.
	status := "reading"
	updated, err := lib.Edit(id, library.Edits{Status: &status})
	if err != nil {
		t.Fatalf("Edit: %v (same-directory Move must not fail)", err)
	}
	if updated.Status() != status {
		t.Errorf("status = %q, want %q", updated.Status(), status)
	}

	if strings.Contains(filepath.Base(updated.EpubPath()), ":") {
		t.Errorf("filename not sanitized after edit: %q", filepath.Base(updated.EpubPath()))
	}
	if _, err := os.Stat(filepath.Join(root, updated.EpubPath())); err != nil {
		t.Errorf("updated EpubPath does not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after self-heal")
	}

	// The bug failed every later edit too.
	status2 := "read"
	if _, err := lib.Edit(id, library.Edits{Status: &status2}); err != nil {
		t.Fatalf("second Edit: %v", err)
	}
}

func TestDeleteRemovesBook(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "To Delete"))
	id := book.ID()
	dir := filepath.Join(cfg.Root, filepath.Dir(book.EpubPath()))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("book directory missing before delete: %v", err)
	}

	if err := lib.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := lib.Search(library.Query{IDs: []int64{id}})
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query returned %d results after delete, want 0", len(results))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("book directory should be removed after delete, stat err = %v", err)
	}
}

// BookDir reads a *Book from many goroutines without a lock, so the library
// must never mutate one it handed out.
func TestSearchSnapshotsAreImmutable(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Before", "Alice"))
	if _, err := lib.Edit(b.ID(), library.Edits{
		Tags:        &[]string{"kept"},
		Series:      new("Saga"),
		SeriesIndex: new("1"),
	}); err != nil {
		t.Fatalf("Edit tags and series: %v", err)
	}

	got, err := lib.Search(library.Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	held := got[0]

	if _, err := lib.Edit(held.ID(), library.Edits{Title: new("After")}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if held.Title() != "Before" {
		t.Errorf("the held snapshot changed to %q; the library mutated a Book it had handed out", held.Title())
	}

	// The getters copy too, so a caller cannot reach back through one.
	authors := held.Authors()
	authors[0].Name = "Mallory"
	if held.Authors()[0].Name != "Alice" {
		t.Error("Authors() handed out the book's own slice; mutating it changed the snapshot")
	}
	tags := held.Tags()
	tags[0] = "injected"
	if held.Tags()[0] != "kept" {
		t.Error("Tags() handed out the book's own slice; mutating it changed the snapshot")
	}

	// Series is a pointer, so its getter must copy the value. SortTitle is here
	// because nothing else reaches it.
	_ = held.SortTitle()
	s := held.Series()
	if s == nil {
		t.Fatal("Series() = nil, want the series set before the search")
	}
	s.Name = "Injected"
	if held.Series().Name != "Saga" {
		t.Error("Series() handed out the book's own SeriesRef; mutating it changed the snapshot")
	}
}

// A rebuild reads meta.toml and the epub, so the index alone cannot show these
// fields were written. The bib fields go into the epub, and the rest into
// meta.toml. The title edit moves the book, so the sidecar has to land in the
// new directory.
func TestEditsSurviveARebuild(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Before", "Alice"))

	tags := []string{"classic", "sf"}
	subjects := []string{"Deserts", "Fiction"}
	contributors := []library.Contributor{{Name: "John Schoenherr", Role: "ill"}}
	if _, err := lib.Edit(b.ID(), library.Edits{
		Title:        new("After"),
		Publisher:    new("Chilton"),
		Rights:       new("All rights reserved"),
		Subjects:     &subjects,
		Contributors: &contributors,
		Status:       new("reading"),
		Rating:       new(4.0),
		Tags:         &tags,
	}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if err := lib.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	got, err := lib.Get(b.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title() != "After" || got.Publisher() != "Chilton" || got.Rights() != "All rights reserved" {
		t.Errorf("after rebuild: title %q, publisher %q, rights %q", got.Title(), got.Publisher(), got.Rights())
	}
	if !slices.Equal(got.Subjects(), subjects) || !slices.Equal(got.Contributors(), contributors) {
		t.Errorf("after rebuild: subjects %v, contributors %+v; want %v, %+v",
			got.Subjects(), got.Contributors(), subjects, contributors)
	}
	if got.Status() != "reading" || got.Rating() != 4 || !slices.Equal(got.Tags(), tags) {
		t.Errorf("after rebuild: status %q, rating %v, tags %v; want reading, 4, %v",
			got.Status(), got.Rating(), got.Tags(), tags)
	}
}

func TestMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)
	const id = 999

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"Get", func() error { _, err := lib.Get(id); return err }},
		{"Edit", func() error { _, err := lib.Edit(id, library.Edits{Status: new("read")}); return err }},
		{"Delete", func() error { return lib.Delete(id) }},
		{"Content", func() error { _, err := lib.Content(id); return err }},
		{"ReadSidecar", func() error { _, err := lib.ReadSidecar(id, "notes.txt"); return err }},
		{"WithSidecars", func() error { return lib.WithSidecars(id, func(*os.Root) error { return nil }) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, library.ErrBookNotFound) {
				t.Errorf("err = %v, want ErrBookNotFound", err)
			}
		})
	}
}

func TestContentServesTheBook(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Open Read"))

	r, err := lib.Content(b.ID())
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	defer r.Close()

	data, err := io.ReadAll(io.NewSectionReader(r, 0, 1<<20))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Errorf("content starts %q, want a zip", data[:min(len(data), 4)])
	}

	cover, err := r.Cover()
	if err != nil {
		t.Fatalf("Cover: %v", err)
	}
	if string(cover) != "placeholder-cover-bytes" {
		t.Errorf("cover = %q, want %q", cover, "placeholder-cover-bytes")
	}

	opf, err := r.OPF()
	if err != nil {
		t.Fatalf("OPF: %v", err)
	}
	if !strings.Contains(string(opf), "Open Read") {
		t.Errorf("OPF does not contain the title: %s", opf)
	}
}
