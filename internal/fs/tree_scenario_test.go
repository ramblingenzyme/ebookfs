// What a client sees of the served tree over a 9P connection.
package fs

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestRootListing(t *testing.T) {
	h := newHarness(t)
	got := h.ls("/")
	want := []string{"books", "by-author", "by-id", "by-series", "by-status", "by-subjects", "by-tag", "ctl", "help", "inbox", "log", "recent", "search", "stats"}
	if !slices.Equal(got, want) {
		t.Errorf("ls / = %v, want %v", got, want)
	}
}

func TestBooksAlreadyInTheLibraryAppear(t *testing.T) {
	h := newHarness(t, ingest("Existing", "Alice"))
	if stat := h.stat("/books/Existing"); stat.Name != "Existing" {
		t.Errorf("stat name = %q, want Existing", stat.Name)
	}
}

func TestEditsReachTheTree(t *testing.T) {
	tests := []struct {
		name      string
		extraSeed func(*testing.T, *library.Library)
		edit      func(*harness)
		gone      []string
		appear    []string
	}{
		{
			name:   "title field",
			edit:   func(h *harness) { h.mustWrite("/books/Dune/title", "Dune Messiah\n") },
			gone:   []string{"/books/Dune", "/by-id/1. Dune", "/by-author/Frank Herbert/Dune"},
			appear: []string{"/books/Dune Messiah", "/by-id/1. Dune Messiah", "/by-author/Frank Herbert/Dune Messiah"},
		},

		{
			name:   "ctl set-status",
			edit:   func(h *harness) { h.mustCtl("set-status reading 1") },
			gone:   []string{"/by-status/unread"},
			appear: []string{"/by-status/reading/Dune"},
		},

		{
			name: "Library.Delete",
			edit: func(h *harness) {
				if err := h.lib.Delete(1); err != nil {
					h.t.Fatalf("Delete: %v", err)
				}
			},
			gone: []string{"/books/Dune", "/by-id/1. Dune", "/by-author/Frank Herbert", "/by-status/unread"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seeds := []func(*testing.T, *library.Library){ingest("Dune", "Frank Herbert")}
			if tt.extraSeed != nil {
				seeds = append(seeds, tt.extraSeed)
			}
			h := newHarness(t, seeds...)

			for _, path := range tt.gone {
				h.mustExist(path)
			}

			tt.edit(h)

			for _, path := range tt.gone {
				h.mustNotExist(path)
			}
			for _, path := range tt.appear {
				h.mustExist(path)
			}
		})
	}
}

// One refusal from the file, which is read-only, and one from the library's
// validation. Both reach the client from the clunk.
func TestRefusedCommitsFailTheClunk(t *testing.T) {
	h := newHarness(t, ingest("Test", "Alice"))

	tests := []struct {
		field string
		value string
	}{
		{"identifiers", "x"},
		{"title", "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			path := "/books/Test/" + tt.field
			before := h.read(path)

			if err := h.write(path, tt.value); err == nil {
				t.Errorf("write %q succeeded, want error", tt.value)
			}

			after := h.read(path)
			if after != before {
				t.Errorf("field changed: before %q, after %q", before, after)
			}
		})
	}
}

func TestLinuxRedirectTruncates(t *testing.T) {
	h := newHarness(t, ingest("Dune Messiah", "Frank Herbert"))

	if err := h.writeLikeLinux("/books/Dune Messiah/title", "Dune\n"); err != nil {
		t.Fatalf("writeLikeLinux: %v", err)
	}
	if got := h.read("/books/Dune/title"); got != "Dune\n" {
		t.Errorf("title = %q, want %q", got, "Dune\n")
	}
}

func TestStatsReportsTheLibrary(t *testing.T) {
	h := newHarness(t, ingest("Test", "Alice"))

	if stats := h.read("/stats"); !strings.Contains(stats, "books: 1\n") {
		t.Errorf("stats = %q, want books: 1", stats)
	}
}

func TestSearchOverNinep(t *testing.T) {
	h := newHarness(t, ingest("Dune", "Frank Herbert"), edited(1, library.Edits{Tags: &[]string{"sf"}}))

	dir := h.search("tag:sf")
	if results := h.ls(dir + "/results"); !slices.Equal(results, []string{"Dune"}) {
		t.Errorf("ls %s/results = %v, want [Dune]", dir, results)
	}
	if ctl := h.read(dir + "/ctl"); !strings.Contains(ctl, "tag:sf") {
		t.Errorf("ctl = %q, want tag:sf", ctl)
	}
}

func TestSearchHandlesAreIndependent(t *testing.T) {
	h := newHarness(t,
		ingest("Dune", "Frank Herbert"), edited(1, library.Edits{Tags: &[]string{"sf"}}),
		ingest("Emma", "Jane Austen"), edited(2, library.Edits{Tags: &[]string{"classic"}}))

	sf, classic := h.search("tag:sf"), h.search("tag:classic")
	if sf == classic {
		t.Fatalf("two searches share the handle %s", sf)
	}
	if got := h.ls(sf + "/results"); !slices.Equal(got, []string{"Dune"}) {
		t.Errorf("tag:sf results = %v, want [Dune]", got)
	}
	if got := h.ls(classic + "/results"); !slices.Equal(got, []string{"Emma"}) {
		t.Errorf("tag:classic results = %v, want [Emma]", got)
	}
}

// Ingest renames the upload into place, so the bytes served are the bytes sent.
func TestUploadReadsBackByteForByte(t *testing.T) {
	h := newHarness(t)
	epub := util.BuildTestEpub(t, "Test Book", "Alice Author")

	if err := h.upload("Test Book.epub", epub); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got := h.readBytes("/books/Test Book/Test Book - Alice Author.epub"); !bytes.Equal(got, epub) {
		t.Errorf("read back %d bytes, want the %d uploaded", len(got), len(epub))
	}
}

func TestCoverOverNinep(t *testing.T) {
	h := newHarness(t, ingest("Dune", "Frank Herbert"))
	path := "/books/Dune/cover.jpg"
	const cover = "placeholder-cover-bytes"

	if got := h.read(path); got != cover {
		t.Fatalf("cover = %q, want %q", got, cover)
	}
	if got := h.stat(path).Length; got != uint64(len(cover)) {
		t.Errorf("cover length = %d, want %d", got, len(cover))
	}

	f, err := h.c.Open(path, proto.Owrite)
	if err != nil {
		t.Fatalf("open for write: %v", err)
	}
	if err := f.Clunk(); err != nil {
		t.Errorf("clunk of an unwritten cover: %v", err)
	}
	if got := h.read(path); got != cover {
		t.Errorf("cover after an empty write = %q, want it unchanged", got)
	}
}
