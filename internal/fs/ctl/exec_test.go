package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// rename-author re-checks the match against the snapshot the search returned,
// so a book the index credits to old but whose snapshot does not is reported
// rather than passed over.
func TestRenameAuthorReportsABookTheSnapshotDoesNotMatch(t *testing.T) {
	book := util.MakeMutableBook(1, "Foundation", "Someone Else")

	search := mock.SearchDeleter{
		SearchFn: func(library.Query) ([]*library.Book, error) {
			return []*library.Book{util.WrapBook(book)}, nil
		},
	}
	edit := mock.Editor{
		EditFn: func(int64, library.Edits) (*library.Book, error) {
			t.Fatal("no book matched, so none should be edited")
			return nil, nil
		},
	}

	cf := newTestCtl(t, search, edit)

	result := cf.execute(`rename-author "Isaac Asimov" "I. Asimov"`)
	const want = "ok: no books renamed\n1 skipped"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
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
			search := mock.SearchDeleter{
				DeleteFn: func(id int64) error {
					t.Fatalf("rejected command still deleted book %d", id)
					return nil
				},
			}
			edit := mock.Editor{
				EditFn: func(id int64, _ library.Edits) (*library.Book, error) {
					t.Fatalf("rejected command still edited book %d", id)
					return nil, nil
				},
			}
			cf := newTestCtl(t, search, edit)

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
