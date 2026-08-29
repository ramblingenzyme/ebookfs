package ctl

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// --- command parsing ---

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input   string
		name    string
		args    []string
		wantErr bool
	}{
		{"add-tag sci-fi 1,2,3", "add-tag", []string{"sci-fi", "1,2,3"}, false},
		{"delete 42", "delete", []string{"42"}, false},
		{`add-tag "science fiction" 1`, "add-tag", []string{"science fiction", "1"}, false},
		{`rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"`, "rename-author", []string{"Asimov", "Isaac Asimov|Asimov, Isaac"}, false},
		{"reindex", "reindex", nil, false},
		{`add-tag "foo 1`, "", nil, true},
		{"", "", nil, true},
	}

	for _, tt := range tests {
		name, args, err := parseCommand(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCommand(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if name != tt.name {
			t.Errorf("parseCommand(%q).name = %q, want %q", tt.input, name, tt.name)
		}
		if !slices.Equal(args, tt.args) {
			t.Errorf("parseCommand(%q).args = %q, want %q", tt.input, args, tt.args)
		}
	}
}

// --- id-spec parsing ---

func TestParseSelection(t *testing.T) {
	tests := []struct {
		spec    string
		all     bool // "*" resolves to an empty Query (every book)
		wantIDs []int64
		wantErr bool
	}{
		{"*", true, nil, false},
		{"1", false, []int64{1}, false},
		{"1,2,3", false, []int64{1, 2, 3}, false},
		{"1, 2, 3", false, []int64{1, 2, 3}, false},
		{"10", false, []int64{10}, false},
		{"", false, nil, true},
		{"abc", false, nil, true},
		{"1,abc", false, nil, true}, // no colon, so it stays an id-spec error
	}

	for _, tt := range tests {
		got, err := parseSelection(tt.spec)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseSelection(%q) error = %v, wantErr = %v", tt.spec, err, tt.wantErr)
			continue
		}
		if tt.wantErr {
			continue
		}
		if tt.all {
			if len(got.IDs) != 0 {
				t.Errorf("parseSelection(%q) IDs = %v, want empty (all books)", tt.spec, got.IDs)
			}
			continue
		}
		if !slices.Equal(got.IDs, tt.wantIDs) {
			t.Errorf("parseSelection(%q) IDs = %v, want %v", tt.spec, got.IDs, tt.wantIDs)
		}
	}
}

// parseSelection falls through to the shared query parser, so a ctl command can
// select by metadata instead of by id.
func TestParseSelectionQuerySyntax(t *testing.T) {
	got, err := parseSelection("tag:sci-fi+status:unread")
	if err != nil {
		t.Fatalf("parseSelection: %v", err)
	}
	// ExactTitles: a ctl selection mutates books, so title: must not match
	// substrings the way the search view does.
	want := model.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}, ExactTitles: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseSelection = %+v, want %+v", got, want)
	}
}

// --- command log ---

func TestCommandLog(t *testing.T) {
	log := NewCommandLog(3)

	// Empty log.
	if entries := log.Entries(); len(entries) != 0 {
		t.Fatalf("empty log entries = %d, want 0", len(entries))
	}

	// Append up to capacity.
	log.Append("cmd1", "ok")
	log.Append("cmd2", "ok")
	log.Append("cmd3", "ok")

	entries := log.Entries()
	if len(entries) != 3 {
		t.Fatalf("log entries = %d, want 3", len(entries))
	}
	if entries[0].Command != "cmd1" {
		t.Errorf("first entry command = %q, want %q", entries[0].Command, "cmd1")
	}
	if entries[2].Command != "cmd3" {
		t.Errorf("third entry command = %q, want %q", entries[2].Command, "cmd3")
	}

	// Overflow: oldest entry ("cmd1") is evicted.
	log.Append("cmd4", "ok")
	entries = log.Entries()
	if len(entries) != 3 {
		t.Fatalf("overflow entries = %d, want 3", len(entries))
	}
	if entries[0].Command != "cmd2" {
		t.Errorf("after overflow, first entry command = %q, want %q", entries[0].Command, "cmd2")
	}
	if entries[2].Command != "cmd4" {
		t.Errorf("after overflow, last entry command = %q, want %q", entries[2].Command, "cmd4")
	}
}

// --- ctl file plumbing ---

// TestCtlFileWriteExecutes drives the file through the 9P Write/Close cycle and
// checks that the command runs and its outcome is recorded in the log, while
// reads return only a usage hint (ctl does not echo command results).
func TestCtlFileWriteExecutes(t *testing.T) {
	called := false
	lib := libfake.Lib{ReindexFn: func() error { called = true; return nil }}
	reg, cmdLog := newTestCtl(t, lib)
	cf := NewCtlFile(reg.FS(), lib, reg, cmdLog)

	// Reading returns a usage hint, not command output.
	got, err := cf.Read(1, 0, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "index rebuilt") {
		t.Fatalf("read should not echo command results, got %q", got)
	}

	// Writing a command and closing the fid executes it...
	if _, err := cf.Write(1, 0, []byte("reindex")); err != nil {
		t.Fatal(err)
	}
	if err := cf.Close(1); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("reindex command was not executed on close")
	}

	// ...and the outcome is recorded in the command log.
	entries := cmdLog.Entries()
	if len(entries) != 1 {
		t.Fatalf("log entries = %d, want 1", len(entries))
	}
	if entries[0].Command != "reindex" {
		t.Errorf("logged command = %q, want reindex", entries[0].Command)
	}
	if !strings.Contains(entries[0].Result, "index rebuilt") {
		t.Errorf("logged result = %q, want reindex result", entries[0].Result)
	}
}

// --- execution ---

func TestAddTag(t *testing.T) {
	book := testutil.MakeMutableBook(1, "Title", "Author")
	book.Meta.Tags = []string{"existing"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if id != 1 {
				t.Fatalf("Edit called with id %d, want 1", id)
			}
			if e.Tags == nil {
				t.Fatal("Edit called with nil Tags")
			}
			if len(*e.Tags) != 2 || (*e.Tags)[0] != "existing" || (*e.Tags)[1] != "newtag" {
				t.Fatalf("Edit called with Tags = %v, want [existing newtag]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`add-tag "newtag" *`, lib, reg, cmdLog)
	if result != "ok: 1 books edited" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books edited")
	}
}

func TestRemoveTag(t *testing.T) {
	book := testutil.MakeMutableBook(2, "Title", "Author")
	book.Meta.Tags = []string{"keep", "remove"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if len(*e.Tags) != 1 || (*e.Tags)[0] != "keep" {
				t.Fatalf("Edit called with Tags = %v, want [keep]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`remove-tag "remove" *`, lib, reg, cmdLog)
	if result != "ok: 1 books edited" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books edited")
	}
}

func TestEditUnknownID(t *testing.T) {
	book := testutil.MakeBook(1, "Title", "Author")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return nil, nil // no book matches id 999
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			t.Fatalf("Edit should not be called for a nonexistent id, got %d", id)
			return nil, nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(book)

	result := execute("add-tag foo 999", lib, reg, cmdLog)
	if !strings.Contains(result, "book 999: not found") {
		t.Fatalf("expected a not-found error for id 999, got %q", result)
	}
}

// A query that names an id alongside a filter must not report the id as
// "not found" when the filter excludes it — the book exists, it just did not
// match. Only a bare id-spec ("1,2,3") gets the typo-catching walk.
func TestAddTagFilteredQueryDoesNotReportNotFound(t *testing.T) {
	book := testutil.MakeBook(1, "Title", "Author")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return nil, nil // book 1 exists but is filtered out by status:read
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			t.Fatalf("Edit should not be called, got %d", id)
			return nil, nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(book)

	result := execute("add-tag foo id:1+status:read", lib, reg, cmdLog)
	if strings.Contains(result, "not found") {
		t.Fatalf("filtered-out id must not be reported as not found, got %q", result)
	}
}

func TestSetStatus(t *testing.T) {
	book := testutil.MakeBook(3, "Title", "Author")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{book}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if e.Status == nil || *e.Status != "reading" {
				t.Fatalf("Edit called with Status = %v, want %q", e.Status, "reading")
			}
			updated := testutil.MakeMutableBook(3, "Title", "Author")
			updated.Meta.Status = *e.Status
			return testutil.WrapBook(updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(book)

	result := execute("set-status reading *", lib, reg, cmdLog)
	if result != "ok: 1 books edited" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books edited")
	}
}

func TestRenameTag(t *testing.T) {
	book := testutil.MakeMutableBook(4, "Title", "Author")
	book.Meta.Tags = []string{"scifi"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if len(*e.Tags) != 1 || (*e.Tags)[0] != "sci-fi" {
				t.Fatalf("Edit called with Tags = %v, want [sci-fi]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`rename-tag "scifi" "sci-fi"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

func TestRenameTagBothTags(t *testing.T) {
	book := testutil.MakeMutableBook(5, "Title", "Author")
	book.Meta.Tags = []string{"old", "other", "new"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if len(*e.Tags) != 2 || !slices.Contains(*e.Tags, "new") || !slices.Contains(*e.Tags, "other") || slices.Contains(*e.Tags, "old") {
				t.Fatalf("rename both: unexpected Tags = %v, want [new other]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`rename-tag "old" "new"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

func TestRenameAuthor(t *testing.T) {
	book := testutil.MakeBook(7, "Title", "Asimov")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			if !slices.Equal(q.Authors, []string{"Asimov"}) {
				t.Errorf("Query.Authors = %q, want [Asimov]", q.Authors)
			}
			return []*library.Book{book}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("rename author: expected one author, got %v", e.Authors)
			}
			a := (*e.Authors)[0]
			if a.Name != "Isaac Asimov" || a.SortName != "Asimov, Isaac" {
				t.Fatalf("rename author: got %+v, want Name=Isaac Asimov SortName=Asimov, Isaac", a)
			}
			updated := testutil.MakeMutableBook(7, "Title", "Asimov")
			updated.Authors = *e.Authors
			return testutil.WrapBook(updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(book)

	result := execute(`rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

func TestRenameAuthorMatchSortName(t *testing.T) {
	book := testutil.MakeMutableBook(8, "Title", "Isaac Asimov")
	book.Authors[0].SortName = "Asimov, Isaac"

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			if !slices.Equal(q.Authors, []string{"Asimov, Isaac"}) {
				t.Errorf("Query.Authors = %q, want [Asimov, Isaac]", q.Authors)
			}
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("expected one author")
			}
			a := (*e.Authors)[0]
			if a.Name != "I. Asimov" || a.SortName != "" {
				t.Fatalf("got %+v, want Name=I. Asimov SortName=\"\"", a)
			}
			updated := *book
			updated.Authors = *e.Authors
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`rename-author "Asimov, Isaac" "I. Asimov"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

func TestRenameSeries(t *testing.T) {
	book := testutil.MakeMutableBook(9, "Title", "Author")
	book.Series = &model.SeriesRef{Name: "Old", Index: "1"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if e.Series == nil || *e.Series != "New" {
				t.Fatalf("renamed series = %v, want %q", e.Series, "New")
			}
			updated := *book
			updated.Series = &model.SeriesRef{Name: *e.Series, Index: book.Series.Index}
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`rename-series "Old" "New"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

// TestRenameAuthorMerge renames an author onto one the book already carries;
// the result must collapse to a single author rather than duplicating it.
func TestRenameAuthorMerge(t *testing.T) {
	book := testutil.MakeMutableBook(11, "Title", "Isaac Asimov")
	book.Authors = append(book.Authors, model.Author{Name: "Paul French"})

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			if !slices.Equal(q.Authors, []string{"Paul French"}) {
				t.Errorf("Query.Authors = %q, want [Paul French]", q.Authors)
			}
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("merge: expected one author, got %v", e.Authors)
			}
			if (*e.Authors)[0].Name != "Isaac Asimov" {
				t.Fatalf("merge: got %q, want Isaac Asimov", (*e.Authors)[0].Name)
			}
			updated := *book
			updated.Authors = *e.Authors
			return testutil.WrapBook(&updated), nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute(`rename-author "Paul French" "Isaac Asimov"`, lib, reg, cmdLog)
	if result != "ok: 1 books renamed" {
		t.Errorf("result = %q, want %q", result, "ok: 1 books renamed")
	}
}

// TestSetRatingUnchanged verifies that setting a rating already in place is a
// no-op: the book is skipped rather than rewritten.
func TestSetRatingUnchanged(t *testing.T) {
	book := testutil.MakeMutableBook(12, "Title", "Author")
	book.Meta.Rating = 4

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*library.Book, error) {
			return []*library.Book{testutil.WrapBook(book)}, nil
		},
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			t.Fatalf("Edit should not be called when the rating is unchanged")
			return nil, nil
		},
	}

	reg, cmdLog := newTestCtl(t, lib)
	reg.Add(testutil.WrapBook(book))

	result := execute("set-rating 4 *", lib, reg, cmdLog)
	if result != "ok: no books edited\n1 skipped" {
		t.Errorf("result = %q, want %q", result, "ok: no books edited\n1 skipped")
	}
}

func TestReindex(t *testing.T) {
	called := false
	lib := libfake.Lib{
		ReindexFn: func() error {
			called = true
			return nil
		},
	}

	reg, cmdLog := newTestCtl(t, libfake.Lib{})
	result := execute("reindex", lib, reg, cmdLog)
	if !called {
		t.Fatal("Reindex not called")
	}
	if result != "ok: index rebuilt" {
		t.Errorf("result = %q, want %q", result, "ok: index rebuilt")
	}
}

// TestDispatch pins that every command name routes to its handler rather than
// falling through to the unknown-command default. Against an empty library the
// results are determinate, so each row asserts the string its handler produces
// — a name that silently stopped being routed would otherwise still look fine.
func TestDispatch(t *testing.T) {
	const notFound = "ok: no books edited\nerrors: 1 book(s)\n  book 1: not found"

	tests := []struct {
		cmd  string
		want string
	}{
		{"add-tag foo 1", notFound},
		{"remove-tag foo 1", notFound},
		{"set-status reading 1", notFound},
		{"set-rating 4 1", notFound},
		{"delete 1", "ok: book 1 deleted"},
		{"reindex", "ok: index rebuilt"},
		{"rename-tag old new", "ok: no books renamed"},
		{"rename-author old new", "ok: no books renamed"},
		{"rename-series old new", "ok: no books renamed"},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			name, args, err := parseCommand(tt.cmd)
			if err != nil {
				t.Fatalf("unexpected parse error for %q: %v", tt.cmd, err)
			}
			got := dispatch(name, args, libfake.Lib{}, registry.NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{}))
			if got != tt.want {
				t.Errorf("dispatch(%q) = %q, want %q", tt.cmd, got, tt.want)
			}
		})
	}
}

// --- command result strings ---

// newTestCtl returns the pieces execute needs, over an empty library.
func newTestCtl(t *testing.T, lib libfake.Lib) (*registry.BookRegistry, *CommandLog) {
	t.Helper()
	return registry.NewBookRegistry(testutil.NewTestFS(t), lib), NewCommandLog(10)
}

// taggedBook builds a minimal book with the given tags, for bulk-edit tests.
func taggedBook(id int64, tags ...string) *library.Book {
	b := testutil.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return testutil.WrapBook(b)
}

// TestCommandRejections pins what a client reads back from ctl when a command
// cannot run: the usage line for a wrong argument count, and the specific error
// for an argument that will not parse. These strings are the whole interface —
// the write succeeds either way, so the result text is the only feedback there
// is — and none of them were asserted before.
func TestCommandRejections(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		// Wrong argument counts: every command answers with its own usage line.
		{"add-tag no args", "add-tag", "usage: add-tag <tag> <id-spec>"},
		{"add-tag one arg", "add-tag onlytag", "usage: add-tag <tag> <id-spec>"},
		{"add-tag three args", "add-tag a b c", "usage: add-tag <tag> <id-spec>"},
		{"remove-tag no args", "remove-tag", "usage: remove-tag <tag> <id-spec>"},
		{"set-status one arg", "set-status read", "usage: set-status <status> <id-spec>"},
		{"set-rating one arg", "set-rating 4", "usage: set-rating <rating> <id-spec>"},
		{"delete no args", "delete", "usage: delete <id>"},
		{"delete two args", "delete 1 2", "usage: delete <id>"},
		{"reindex with args", "reindex now", "usage: reindex"},
		{"rename-tag one arg", "rename-tag old", "usage: rename-tag <old> <new>"},
		{"rename-author one arg", "rename-author old", "usage: rename-author <old> <new>"},
		{"rename-series one arg", "rename-series old", "usage: rename-series <old> <new>"},

		// Arguments that parse-check before any library call.
		{"invalid rating", "set-rating high *", `error: invalid rating "high"`},
		{"invalid delete id", "delete abc", `error: invalid id "abc"`},
		// ParseAuthor splits on "|", so a spec that supplies only a sort name
		// leaves the display name empty. A bare "" never gets this far —
		// parseCommand drops empty arguments, which lands on the usage line.
		{"sort name but no display name", `rename-author "Old" "|Doe, Jane"`, "error: new author name must not be empty"},
		{"whitespace-only new author", `rename-author "Old" " "`, "error: new author name must not be empty"},

		{"unknown command", "frobnicate", `error: unknown command "frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// EditFn and DeleteFn are left unstubbed: a rejected command must
			// not reach the library at all, and libfake errors if it does.
			lib := libfake.Lib{
				EditFn: func(id int64, _ library.Edits) (*library.Book, error) {
					t.Fatalf("rejected command still edited book %d", id)
					return nil, nil
				},
				DeleteFn: func(id int64) error {
					t.Fatalf("rejected command still deleted book %d", id)
					return nil
				},
			}
			reg, cmdLog := newTestCtl(t, lib)

			if got := execute(tc.cmd, lib, reg, cmdLog); got != tc.want {
				t.Errorf("execute(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestCommandRejectionsAreLogged pins that a refusal is recorded like any other
// command. The log is how a client sees what happened after the fact, so a
// rejection that never reaches it is a command that silently did nothing.
func TestCommandRejectionsAreLogged(t *testing.T) {
	lib := libfake.Lib{}
	reg, cmdLog := newTestCtl(t, lib)

	execute("delete abc", lib, reg, cmdLog)

	entries := cmdLog.Entries()
	if len(entries) != 1 {
		t.Fatalf("log holds %d entries, want the rejected command recorded", len(entries))
	}
	if entries[0].Command != "delete abc" {
		t.Errorf("logged command = %q, want %q", entries[0].Command, "delete abc")
	}
	if !strings.Contains(entries[0].Result, "invalid id") {
		t.Errorf("logged result = %q, want the rejection reason", entries[0].Result)
	}
}

// TestCommandSuccessStrings pins the other half: what a command reports when it
// works. The counts are the only signal that a bulk edit did what was asked, so
// "ok: no books edited" must not read the same as "ok: 2 books edited".
func TestCommandSuccessStrings(t *testing.T) {
	tests := []struct {
		name  string
		books []*library.Book
		cmd   string
		want  string
	}{
		{
			"one book edited",
			[]*library.Book{taggedBook(1)},
			`add-tag "new" *`,
			"ok: 1 books edited",
		},
		{
			"several books edited",
			[]*library.Book{taggedBook(1), taggedBook(2)},
			`add-tag "new" *`,
			"ok: 2 books edited",
		},
		{
			"book already tagged",
			[]*library.Book{taggedBook(1, "new")},
			`add-tag "new" *`,
			"ok: no books edited\n1 skipped",
		},
		{
			"mixed edited and skipped",
			[]*library.Book{taggedBook(1), taggedBook(2, "new")},
			`add-tag "new" *`,
			"ok: 1 books edited\n1 skipped",
		},
		{
			"selection matches nothing",
			nil,
			`add-tag "new" *`,
			"ok: no books edited",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lib := libfake.Lib{
				SearchFn: func(model.Query) ([]*library.Book, error) { return tc.books, nil },
				EditFn: func(id int64, e library.Edits) (*library.Book, error) {
					for _, b := range tc.books {
						if b.ID() == id {
							// Create a mutable copy of the book
							updated := testutil.MakeMutableBook(b.ID(), b.Title(), "Author")
							updated.Meta.Tags = b.Tags()
							if e.Tags != nil {
								updated.Meta.Tags = *e.Tags
							}
							return testutil.WrapBook(updated), nil
						}
					}
					return nil, fmt.Errorf("no book %d", id)
				},
			}
			reg, cmdLog := newTestCtl(t, lib)
			for _, b := range tc.books {
				reg.Add(b)
			}

			if got := execute(tc.cmd, lib, reg, cmdLog); got != tc.want {
				t.Errorf("execute(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}

// TestCommandFailureStrings pins the reporting when the library refuses the
// work. A per-book failure must be named and counted rather than folded into
// the success line, or a bulk edit that half-failed reads as a clean run.
func TestCommandFailureStrings(t *testing.T) {
	t.Run("edit fails for one book", func(t *testing.T) {
		books := []*library.Book{testutil.MakeBook(1, "A", "Author"), testutil.MakeBook(2, "B", "Author")}
		lib := libfake.Lib{
			SearchFn: func(model.Query) ([]*library.Book, error) { return books, nil },
			EditFn: func(id int64, e library.Edits) (*library.Book, error) {
				if id == 2 {
					return nil, errors.New("disk on fire")
				}
				// Create a mutable copy of the book
				updated := testutil.MakeMutableBook(books[0].ID(), books[0].Title(), "Author")
				updated.Meta.Tags = books[0].Tags()
				updated.Meta.Tags = *e.Tags
				return testutil.WrapBook(updated), nil
			},
		}
		reg, cmdLog := newTestCtl(t, lib)
		for _, b := range books {
			reg.Add(b)
		}

		got := execute(`add-tag "new" *`, lib, reg, cmdLog)

		want := "ok: 1 books edited\nerrors: 1 book(s)\n  book 2: disk on fire"
		if got != want {
			t.Errorf("execute = %q, want %q", got, want)
		}
	})

	t.Run("search fails", func(t *testing.T) {
		lib := libfake.Lib{
			SearchFn: func(model.Query) ([]*library.Book, error) { return nil, errors.New("index closed") },
		}
		reg, cmdLog := newTestCtl(t, lib)

		if got := execute(`add-tag "new" *`, lib, reg, cmdLog); got != "error: query failed: index closed" {
			t.Errorf("execute = %q, want the query failure surfaced", got)
		}
	})

	t.Run("delete fails", func(t *testing.T) {
		lib := libfake.Lib{DeleteFn: func(int64) error { return errors.New("still open") }}
		reg, cmdLog := newTestCtl(t, lib)

		if got := execute("delete 7", lib, reg, cmdLog); got != "error: book 7: still open" {
			t.Errorf("execute = %q, want the delete failure surfaced", got)
		}
	})

	t.Run("reindex fails", func(t *testing.T) {
		lib := libfake.Lib{ReindexFn: func() error { return errors.New("duplicate id 3") }}
		reg, cmdLog := newTestCtl(t, lib)

		if got := execute("reindex", lib, reg, cmdLog); got != "error: duplicate id 3" {
			t.Errorf("execute = %q, want the reindex failure surfaced", got)
		}
	})

	t.Run("rename query fails", func(t *testing.T) {
		lib := libfake.Lib{
			SearchFn: func(model.Query) ([]*library.Book, error) { return nil, errors.New("index closed") },
		}
		reg, cmdLog := newTestCtl(t, lib)

		for _, cmd := range []string{`rename-tag "a" "b"`, `rename-author "a" "b"`, `rename-series "a" "b"`} {
			if got := execute(cmd, lib, reg, cmdLog); got != "error: query failed: index closed" {
				t.Errorf("execute(%q) = %q, want the query failure surfaced", cmd, got)
			}
		}
	})
}

// TestSingleBookCommandSuccessStrings covers the two commands that report on
// one book rather than a selection.
func TestSingleBookCommandSuccessStrings(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		var deleted int64
		lib := libfake.Lib{DeleteFn: func(id int64) error { deleted = id; return nil }}
		reg, cmdLog := newTestCtl(t, lib)

		if got := execute("delete 7", lib, reg, cmdLog); got != "ok: book 7 deleted" {
			t.Errorf("execute = %q, want %q", got, "ok: book 7 deleted")
		}
		if deleted != 7 {
			t.Errorf("Delete called with id %d, want 7", deleted)
		}
	})

	t.Run("reindex", func(t *testing.T) {
		lib := libfake.Lib{ReindexFn: func() error { return nil }}
		reg, cmdLog := newTestCtl(t, lib)

		if got := execute("reindex", lib, reg, cmdLog); got != "ok: index rebuilt" {
			t.Errorf("execute = %q, want %q", got, "ok: index rebuilt")
		}
	})
}
