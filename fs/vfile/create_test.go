package vfile

import (
	"errors"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// creatorDir is a directory that accepts creates, recording what it was asked
// for so the dispatch can be checked rather than inferred.
type creatorDir struct {
	fs.Dir
	gotName string
	gotPerm uint32
	gotMode uint8
	err     error
}

func (d *creatorDir) Create(f *fs.FS, name string, perm uint32, mode uint8) (fs.File, error) {
	d.gotName, d.gotPerm, d.gotMode = name, perm, mode
	if d.err != nil {
		return nil, d.err
	}
	return fs.NewBaseFile(NewStat(f, name, perm)), nil
}

// TestDispatchCreate covers the FS-wide create hook. Create policy lives on each
// directory rather than in one central switch, so the hook's whole job is to
// route to the parent and refuse when the parent has no policy.
func TestDispatchCreate(t *testing.T) {
	f := testutil.NewTestFS(t)

	t.Run("delegates to a parent that accepts creates", func(t *testing.T) {
		parent := &creatorDir{Dir: fs.NewStaticDir(NewStat(f, "inbox", 0777))}

		file, err := DispatchCreate(f, parent, "glenda", "book.epub", 0644, 1)
		if err != nil {
			t.Fatalf("DispatchCreate: %v", err)
		}
		if file == nil {
			t.Fatal("DispatchCreate returned no file and no error")
		}
		if parent.gotName != "book.epub" || parent.gotPerm != 0644 || parent.gotMode != 1 {
			t.Errorf("parent received (%q, %o, %d), want (%q, %o, %d)",
				parent.gotName, parent.gotPerm, parent.gotMode, "book.epub", 0644, 1)
		}
	})

	t.Run("propagates the parent's refusal", func(t *testing.T) {
		parent := &creatorDir{Dir: fs.NewStaticDir(NewStat(f, "inbox", 0777)), err: errors.New("not an epub")}

		if _, err := DispatchCreate(f, parent, "glenda", "notes.txt", 0644, 1); err == nil {
			t.Error("DispatchCreate returned nil, want the parent's refusal surfaced")
		}
	})

	t.Run("refuses a parent that does not accept creates", func(t *testing.T) {
		// Every view directory is this case: the tree is derived from the
		// library, so a create anywhere but the inbox has nowhere to go.
		plain := fs.NewStaticDir(NewStat(f, "by-author", 0555))

		if _, err := DispatchCreate(f, plain, "glenda", "book.epub", 0644, 1); err == nil {
			t.Error("DispatchCreate succeeded on a directory with no create policy, want it refused")
		}
	})
}
