package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// A rename re-checks the match against the book's state at the edit, so a book
// that lost the old value after the search is skipped rather than renamed.
func TestRenameSkipsABookThatChangedSinceTheSearch(t *testing.T) {
	// The search claims a match, but the book carries no tag or series and a
	// different author.
	book := util.WrapBook(util.MakeMutableBook(1, "Foundation", "Someone Else"))

	for _, cmd := range []string{
		`rename-author "Isaac Asimov" "I. Asimov"`,
		`rename-tag "scifi" "sci-fi"`,
		`rename-series "Old" "New"`,
	} {
		t.Run(cmd, func(t *testing.T) {
			cf := newTestCtl(t, mock.Library{
				SearchDeleter: mock.SearchDeleter{
					SearchFn: func(library.Query) ([]*library.Book, error) {
						return []*library.Book{book}, nil
					},
				},
				UpdateFn: func(_ int64, fn func(*library.Book) *library.Edits) (*library.Book, bool, error) {
					if e := fn(book); e != nil {
						t.Fatalf("the book no longer matches, so it should not be edited: %+v", e)
					}
					return book, false, nil
				},
			})

			const want = "ok: no books renamed\n1 skipped"
			if got := cf.execute(cmd); got != want {
				t.Errorf("result = %q, want %q", got, want)
			}
		})
	}
}

// What a client reads back from ctl when a command cannot run: the usage line
// for a wrong argument count, and the specific error for an argument that will
// not parse. These strings are the whole interface, since the write succeeds
// either way and the result text is the only feedback there is. A refusal is
// logged like any other command, or it silently did nothing.
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
		{"rename-tag one arg", "rename-tag old", "usage: rename-tag <old> <new>"},
		{"rename-author one arg", "rename-author old", "usage: rename-author <old> <new>"},
		{"rename-series one arg", "rename-series old", "usage: rename-series <old> <new>"},

		// Arguments that parse-check before any library call.
		{"invalid rating", "set-rating high *", `error: invalid rating "high"`},
		{"invalid delete id", "delete abc", `error: invalid id "abc"`},
		// ParseAuthor splits on "|", so a spec that supplies only a sort name
		// leaves the display name empty. A bare "" never gets this far:
		// parseCommand drops empty arguments, which lands on the usage line.
		{"sort name but no display name", `rename-author "Old" "|Doe, Jane"`, "error: new author name must not be empty"},
		{"whitespace-only new author", `rename-author "Old" " "`, "error: new author name must not be empty"},

		{"unknown command", "frobnicate", `error: unknown command "frobnicate"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Both hooks fail the test rather than returning: a rejected
			// command must not reach the library at all.
			cf := newTestCtl(t, mock.Library{
				SearchDeleter: mock.SearchDeleter{
					DeleteFn: func(id int64) error {
						t.Fatalf("rejected command still deleted book %d", id)
						return nil
					},
				},
				UpdateFn: func(id int64, _ func(*library.Book) *library.Edits) (*library.Book, bool, error) {
					t.Fatalf("rejected command still edited book %d", id)
					return nil, false, nil
				},
			})

			if got := cf.execute(tc.cmd); got != tc.want {
				t.Errorf("execute(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
			entries := cf.cmdLog.Entries()
			if len(entries) != 1 || entries[0].Command != tc.cmd || entries[0].Result != tc.want {
				t.Errorf("log = %+v, want the one refusal recorded", entries)
			}
		})
	}
}
