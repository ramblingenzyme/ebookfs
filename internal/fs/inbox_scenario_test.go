// inbox/ over a 9P connection: an upload reaches the views, a failed one
// leaves nothing, and only inbox/ accepts a create.
package fs

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestInboxUploadReachesViews(t *testing.T) {
	h := newHarness(t)
	data := util.BuildTestEpub(t, "Ingested", "Alice")

	if err := h.upload("book.epub", data); err != nil {
		t.Fatalf("upload: %v", err)
	}

	books := h.ls("/books")
	if len(books) != 1 {
		t.Errorf("/books has %d entries, want 1", len(books))
	}
	if len(books) > 0 && books[0] != "Ingested" {
		t.Errorf("/books[0] = %q, want Ingested", books[0])
	}

	byAuthor := h.ls("/by-author")
	if len(byAuthor) != 1 {
		t.Errorf("/by-author has %d entries, want 1", len(byAuthor))
	}
	if len(byAuthor) > 0 && byAuthor[0] != "Alice" {
		t.Errorf("/by-author[0] = %q, want Alice", byAuthor[0])
	}

	inbox := h.ls("/inbox")
	if len(inbox) != 0 {
		t.Errorf("/inbox has %d entries, want 0", len(inbox))
	}
}

func TestInboxFailedIngestLeavesNoBook(t *testing.T) {
	h := newHarness(t)

	if err := h.upload("bad.epub", []byte("not an epub")); err == nil {
		t.Fatal("upload succeeded on an ingest that failed")
	}

	books := h.ls("/books")
	if len(books) != 0 {
		t.Errorf("/books has %d entries, want 0", len(books))
	}
}

func TestCreateOutsideTheInboxIsRefused(t *testing.T) {
	h := newHarness(t)

	if _, err := h.c.Create("/books/x.epub", 0644, proto.Owrite); err == nil {
		t.Error("creating /books/x.epub was accepted")
	}
	if got := h.ls("/books"); len(got) != 0 {
		t.Errorf("ls /books = %v, want empty", got)
	}
}
