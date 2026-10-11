// ctl over a 9P connection: what a client sees of a command in the tree and
// the log.
package fs

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestCtlUnknownCommandLeavesTheTreeAlone(t *testing.T) {
	h := newHarness(t, ingest("Test", "Alice"))

	before := h.tree("/")
	if result := h.ctl("explode 1"); !strings.Contains(result, "unknown command") {
		t.Errorf("ctl result = %q, want 'unknown command'", result)
	}
	if after := h.tree("/"); !slices.Equal(before, after) {
		t.Errorf("tree changed after an unknown command\nbefore: %v\nafter: %v", before, after)
	}
}

// Reading ctl returns a usage hint and commits nothing, or opening it to read
// would run an empty command.
func TestReadingCtlRunsNothing(t *testing.T) {
	h := newHarness(t)

	if got := h.read("/ctl"); got == "" || strings.Contains(got, "ok:") {
		t.Errorf("read /ctl = %q, want a usage hint", got)
	}
	if log := h.read("/log"); log != "(no commands yet)\n" {
		t.Errorf("log = %q, want %q", log, "(no commands yet)\n")
	}
}

// Each command against a real library, checked by what the book holds after.
// Between them the rows reach every command's handler.
func TestCommandEdits(t *testing.T) {
	asimov := []library.Author{{Name: "Isaac Asimov", SortName: "Asimov, Isaac"}}
	for _, tc := range []struct {
		name    string
		authors []string
		edits   library.Edits
		cmd     string
		result  string
		check   func(t *testing.T, b *library.Book)
	}{
		{"add-tag", nil, library.Edits{Tags: &[]string{"existing"}},
			`add-tag "newtag" *`, "ok: 1 books edited",
			wantTags("existing", "newtag")},
		{"remove-tag", nil, library.Edits{Tags: &[]string{"keep", "remove"}},
			`remove-tag "remove" *`, "ok: 1 books edited",
			wantTags("keep")},
		{"remove-tag the book does not have", nil, library.Edits{Tags: &[]string{"keep"}},
			`remove-tag "absent" *`, "ok: no books edited\n1 skipped",
			wantTags("keep")},
		// A bare id list is walked as given, so a typo surfaces.
		{"bare id that does not exist", nil, library.Edits{},
			"add-tag foo 99", "error: no books edited\nerrors: 1 book(s)\n  book 99: not found",
			wantTags()},
		{"set-status", nil, library.Edits{},
			"set-status reading *", "ok: 1 books edited",
			func(t *testing.T, b *library.Book) {
				if b.Status() != "reading" {
					t.Errorf("status = %q, want reading", b.Status())
				}
			}},
		{"set-rating", nil, library.Edits{},
			"set-rating 4 *", "ok: 1 books edited",
			wantRating(4)},
		// The skip shows in the result, since an edit would also bump the
		// modification time.
		{"set-rating already in place", nil, library.Edits{Rating: new(4.0)},
			"set-rating 4 *", "ok: no books edited\n1 skipped",
			wantRating(4)},
		// A query naming an id alongside a filter must not report the id as not
		// found when the filter excludes it. Only a bare id list gets that check.
		{"filtered-out id is not reported missing", nil, library.Edits{},
			"add-tag foo id:1+status:read", "ok: no books edited",
			wantTags()},
		{"rename-tag", nil, library.Edits{Tags: &[]string{"scifi"}},
			`rename-tag "scifi" "sci-fi"`, "ok: 1 books renamed",
			wantTags("sci-fi")},
		{"rename-tag onto a tag the book has", nil, library.Edits{Tags: &[]string{"old", "other", "new"}},
			`rename-tag "old" "new"`, "ok: 1 books renamed",
			wantTags("new", "other")},
		{"rename-author with a sort name", []string{"Asimov"}, library.Edits{},
			`rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"`, "ok: 1 books renamed",
			wantAuthors(library.Author{Name: "Isaac Asimov", SortName: "Asimov, Isaac"})},
		{"rename-author matched by sort name", nil, library.Edits{Authors: &asimov},
			`rename-author "Asimov, Isaac" "I. Asimov"`, "ok: 1 books renamed",
			wantAuthors(library.Author{Name: "I. Asimov"})},
		// Renaming onto an author the book already carries collapses to one.
		{"rename-author merges", []string{"Isaac Asimov", "Paul French"}, library.Edits{},
			`rename-author "Paul French" "Isaac Asimov"`, "ok: 1 books renamed",
			wantAuthors(library.Author{Name: "Isaac Asimov"})},
		{"rename-series", nil, library.Edits{Series: new("Old"), SeriesIndex: new("1")},
			`rename-series "Old" "New"`, "ok: 1 books renamed",
			func(t *testing.T, b *library.Book) {
				if b.SeriesName() != "New" || b.SeriesIndex() != "1" {
					t.Errorf("series = %q at %q, want New at 1", b.SeriesName(), b.SeriesIndex())
				}
			}},
		{"delete", nil, library.Edits{},
			"delete 1", "ok: book 1 deleted",
			nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.authors == nil {
				tc.authors = []string{"Author"}
			}
			books := seeds(ingest("Title", tc.authors...))
			if tc.edits != (library.Edits{}) {
				books = append(books, edited(1, tc.edits))
			}
			h := newHarness(t, books...)

			if got := h.ctl(tc.cmd); got != tc.result {
				t.Errorf("ctl %q = %q, want %q", tc.cmd, got, tc.result)
			}
			b, err := h.lib.Get(1)
			if tc.check == nil {
				if !errors.Is(err, library.ErrBookNotFound) {
					t.Errorf("Get after delete: err = %v, want ErrBookNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			tc.check(t, b)
		})
	}
}

func wantTags(want ...string) func(*testing.T, *library.Book) {
	return func(t *testing.T, b *library.Book) {
		t.Helper()
		got := b.Tags()
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("tags = %v, want %v", got, want)
		}
	}
}

func wantRating(want float64) func(*testing.T, *library.Book) {
	return func(t *testing.T, b *library.Book) {
		t.Helper()
		if b.Rating() != want {
			t.Errorf("rating = %v, want %v", b.Rating(), want)
		}
	}
}

func wantAuthors(want ...library.Author) func(*testing.T, *library.Book) {
	return func(t *testing.T, b *library.Book) {
		t.Helper()
		got := b.Authors()
		if len(got) != len(want) {
			t.Fatalf("authors = %+v, want %+v", got, want)
		}
		for i := range got {
			if got[i].Name != want[i].Name || got[i].SortName != want[i].SortName {
				t.Errorf("authors = %+v, want %+v", got, want)
			}
		}
	}
}

// The counts are the only signal that a bulk edit did what was asked, so "ok:
// no books edited" must not read the same as "ok: 2 books edited".
func TestCommandResults(t *testing.T) {
	tagNew := library.Edits{Tags: &[]string{"new"}}
	for _, tc := range []struct {
		name  string
		books []func(*testing.T, *library.Library)
		cmd   string
		want  string
	}{
		{"one book edited",
			seeds(ingest("Plain", "Author")),
			`add-tag "new" *`, "ok: 1 books edited"},
		{"several books edited",
			seeds(ingest("Plain", "Author"), ingest("Other", "Author")),
			`add-tag "new" *`, "ok: 2 books edited"},
		{"book already tagged",
			seeds(ingest("Tagged", "Author"), edited(1, tagNew)),
			`add-tag "new" *`, "ok: no books edited\n1 skipped"},
		{"mixed edited and skipped",
			seeds(ingest("Plain", "Author"), ingest("Tagged", "Author"), edited(2, tagNew)),
			`add-tag "new" *`, "ok: 1 books edited\n1 skipped"},
		{"selection matches nothing", nil,
			`add-tag "new" *`, "ok: no books edited"},
		{"rename matches nothing",
			seeds(ingest("Plain", "Author")),
			`rename-tag "absent" "x"`, "ok: no books renamed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.books...)
			if got := h.ctl(tc.cmd); got != tc.want {
				t.Errorf("ctl %q = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

func seeds(s ...func(*testing.T, *library.Library)) []func(*testing.T, *library.Library) {
	return s
}

// What the library's failures look like to a client. A per-book failure is
// named and counted rather than folded into the success line. The messages
// are the library's, so only their shape is checked.
func TestCommandFailures(t *testing.T) {
	// The index still lists a book whose directory was removed outside ebookfs,
	// and an edit to it fails writing meta.toml.
	removeBook := func(id int64) func(*testing.T, *harness) {
		return func(t *testing.T, h *harness) {
			b, err := h.lib.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(h.root, b.Dir())); err != nil {
				t.Fatal(err)
			}
		}
	}
	closeLibrary := func(t *testing.T, h *harness) { h.lib.Close() }

	for _, tc := range []struct {
		name   string
		books  []func(*testing.T, *library.Library)
		breaks func(*testing.T, *harness)
		cmd    string
		want   []string // the result's lines, each a prefix
	}{
		{"edit fails for one book",
			seeds(ingest("Kept", "Author"), ingest("Gone", "Author")), removeBook(2),
			`add-tag "new" *`, []string{"ok: 1 books edited", "errors: 1 book(s)", "  book 2: "}},
		{"edit fails for every book",
			seeds(ingest("Gone", "Author")), removeBook(1),
			`add-tag "new" *`, []string{"error: no books edited", "errors: 1 book(s)", "  book 1: "}},
		{"search fails", nil, closeLibrary,
			`add-tag "new" *`, []string{"error: query failed: "}},
		{"rename-tag query fails", nil, closeLibrary,
			`rename-tag "a" "b"`, []string{"error: query failed: "}},
		{"rename-author query fails", nil, closeLibrary,
			`rename-author "a" "b"`, []string{"error: query failed: "}},
		{"rename-series query fails", nil, closeLibrary,
			`rename-series "a" "b"`, []string{"error: query failed: "}},
		{"delete a missing book", nil, nil,
			"delete 1", []string{"error: book 1: not found"}},
		{"delete fails",
			seeds(ingest("Kept", "Author")), func(t *testing.T, h *harness) {
				if os.Geteuid() == 0 {
					t.Skip("root ignores directory permissions")
				}
				b, _ := h.lib.Get(1)
				dir := filepath.Join(h.root, b.Dir())
				if err := os.Chmod(dir, 0555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(dir, 0755) })
			}, "delete 1", []string{"error: book 1: "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.books...)
			if tc.breaks != nil {
				tc.breaks(t, h)
			}

			got := h.ctl(tc.cmd)
			lines := strings.Split(got, "\n")
			if len(lines) != len(tc.want) {
				t.Fatalf("ctl %q = %q, want %d lines starting %q", tc.cmd, got, len(tc.want), tc.want)
			}
			for i, prefix := range tc.want {
				if !strings.HasPrefix(lines[i], prefix) {
					t.Errorf("ctl %q line %d = %q, want it to start %q", tc.cmd, i, lines[i], prefix)
				}
			}
		})
	}
}
