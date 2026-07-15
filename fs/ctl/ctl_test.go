package ctl

import (
	"slices"
	"strings"
	"testing"

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
		got, err := parseCommand(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCommand(%q) error = %v, wantErr = %v", tt.input, err, tt.wantErr)
			continue
		}
		if got.name != tt.name {
			t.Errorf("parseCommand(%q).name = %q, want %q", tt.input, got.name, tt.name)
		}
		if !slices.Equal(got.args, tt.args) {
			t.Errorf("parseCommand(%q).args = %q, want %q", tt.input, got.args, tt.args)
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
	f := testutil.NewTestFS(t)
	called := false
	lib := libfake.Lib{ReindexFn: func() error { called = true; return nil }}
	reg := registry.NewBookRegistry(f, lib)
	cmdLog := NewCommandLog(10)
	cf := NewCtlFile(f, lib, reg, cmdLog)

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
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(1, "Title", "Author")
	book.Meta.Tags = []string{"existing"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
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
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`add-tag "newtag" *`, lib, reg, cmdLog)

	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRemoveTag(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(2, "Title", "Author")
	book.Meta.Tags = []string{"keep", "remove"}

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if len(*e.Tags) != 1 || (*e.Tags)[0] != "keep" {
				t.Fatalf("Edit called with Tags = %v, want [keep]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`remove-tag "remove" *`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestEditUnknownID(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(1, "Title", "Author")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*model.Book, error) {
			return nil, nil // no book matches id 999
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			t.Fatalf("Edit should not be called for a nonexistent id, got %d", id)
			return nil, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute("add-tag foo 999", lib, reg, cmdLog)
	if !strings.Contains(result, "book 999: not found") {
		t.Fatalf("expected a not-found error for id 999, got %q", result)
	}
}

func TestSetStatus(t *testing.T) {
	f := testutil.NewTestFS(t)
	book := testutil.MakeBook(3, "Title", "Author")

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if e.Status == nil || *e.Status != "reading" {
				t.Fatalf("Edit called with Status = %v, want %q", e.Status, "reading")
			}
			updated := *book
			updated.Meta.Status = *e.Status
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute("set-status reading *", lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRenameTag(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(4, "Title", "Author")
	book.Meta.Tags = []string{"scifi"}

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			// renameTag queries by tag filter.
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if len(*e.Tags) != 1 || (*e.Tags)[0] != "sci-fi" {
				t.Fatalf("Edit called with Tags = %v, want [sci-fi]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-tag "scifi" "sci-fi"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRenameTagBothTags(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(5, "Title", "Author")
	book.Meta.Tags = []string{"old", "other", "new"}

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			// Should remove "old" but keep "new" and "other".
			if len(*e.Tags) != 2 || !slices.Contains(*e.Tags, "new") || !slices.Contains(*e.Tags, "other") || slices.Contains(*e.Tags, "old") {
				t.Fatalf("rename both: unexpected Tags = %v, want [new other]", *e.Tags)
			}
			updated := *book
			updated.Meta.Tags = *e.Tags
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-tag "old" "new"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRenameAuthor(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(7, "Title", "Asimov")

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("rename author: expected one author, got %v", e.Authors)
			}
			a := (*e.Authors)[0]
			if a.Name != "Isaac Asimov" || a.SortName != "Asimov, Isaac" {
				t.Fatalf("rename author: got %+v, want Name=Isaac Asimov SortName=Asimov, Isaac", a)
			}
			updated := *book
			updated.Authors = *e.Authors
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRenameAuthorMatchSortName(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(8, "Title", "Isaac Asimov")
	book.Authors[0].SortName = "Asimov, Isaac"

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("expected one author")
			}
			a := (*e.Authors)[0]
			// Sort name is cleared when not specified in the new value.
			if a.Name != "I. Asimov" || a.SortName != "" {
				t.Fatalf("got %+v, want Name=I. Asimov SortName=\"\"", a)
			}
			updated := *book
			updated.Authors = *e.Authors
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-author "Asimov, Isaac" "I. Asimov"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestRenameSeries(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(9, "Title", "Author")
	book.Series = &model.SeriesRef{Name: "Old", Index: 1.0}

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if e.Series == nil || *e.Series != "New" {
				t.Fatalf("renamed series = %v, want %q", e.Series, "New")
			}
			updated := *book
			updated.Series = &model.SeriesRef{Name: *e.Series, Index: book.Series.Index}
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-series "Old" "New"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

// TestRenameAuthorMerge renames an author onto one the book already carries;
// the result must collapse to a single author rather than duplicating it.
func TestRenameAuthorMerge(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(11, "Title", "Isaac Asimov")
	book.Authors = append(book.Authors, model.Author{Name: "Paul French"})

	lib := libfake.Lib{
		QueryFn: func(f model.Filter) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			if e.Authors == nil || len(*e.Authors) != 1 {
				t.Fatalf("merge: expected one author, got %v", e.Authors)
			}
			if (*e.Authors)[0].Name != "Isaac Asimov" {
				t.Fatalf("merge: got %q, want Isaac Asimov", (*e.Authors)[0].Name)
			}
			updated := *book
			updated.Authors = *e.Authors
			return &updated, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute(`rename-author "Paul French" "Isaac Asimov"`, lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

// TestSetRatingUnchanged verifies that setting a rating already in place is a
// no-op: the book is skipped rather than rewritten.
func TestSetRatingUnchanged(t *testing.T) {
	f := testutil.NewTestFS(t)

	book := testutil.MakeBook(12, "Title", "Author")
	book.Meta.Rating = 4

	lib := libfake.Lib{
		SearchFn: func(q model.Query) ([]*model.Book, error) {
			return []*model.Book{book}, nil
		},
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			t.Fatalf("Edit should not be called when the rating is unchanged")
			return nil, nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	reg.Add(book)

	cmdLog := NewCommandLog(10)
	result := execute("set-rating 4 *", lib, reg, cmdLog)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestUnknownCommand(t *testing.T) {
	cmdLog := NewCommandLog(10)
	result := execute("nonsense", libfake.Lib{}, registry.NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{}), cmdLog)
	if result == "" {
		t.Fatal("expected error message")
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

	cmdLog := NewCommandLog(10)
	result := execute("reindex", lib, registry.NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{}), cmdLog)
	if !called {
		t.Fatal("Reindex not called")
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		cmd string
	}{
		{"add-tag foo 1"},
		{"remove-tag foo 1"},
		{"set-status reading 1"},
		{"set-rating 4 1"},
		{"delete 1"},
		{"reindex"},
		{"rename-tag old new"},
		{"rename-author old new"},
		{"rename-series old new"},
	}

	for _, tt := range tests {
		p, err := parseCommand(tt.cmd)
		if err != nil {
			t.Fatalf("unexpected parse error for %q: %v", tt.cmd, err)
		}
		// dispatch should not panic; we just check it handles every command.
		got := dispatch(p, libfake.Lib{}, registry.NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{}))
		if got == "" {
			t.Errorf("dispatch(%q) returned empty result", tt.cmd)
		}
	}
}
