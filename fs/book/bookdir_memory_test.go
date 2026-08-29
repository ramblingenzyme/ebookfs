package book

import (
	"fmt"
	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// representativeBook builds a Book whose field sizes match a realistic library
// entry: ~40-char title, one author with sort name, a series, three tags, two
// identifiers, a ~300-char description, and populated path fields. The id is
// baked into the title so each book's backing strings are distinct (no interning
// hiding real per-book cost).
func representativeBook(id int64, withCover bool) *library.Book {
	title := fmt.Sprintf("The Left Hand of Darkness Volume %d", id)
	desc := "A linguist negotiates a diplomatic mission to the planet Winter, " +
		"whose inhabitants can change sex at will. A study of gender, loyalty, " +
		"and otherness, and one of the foundational works of modern science " +
		"fiction. Winner of the Hugo and Nebula awards."
	bib := bookmodel.Bib{
		Title:     title,
		SortTitle: title,
		Authors: []model.Author{{
			Name:     "Ursula K. Le Guin",
			SortName: "Le Guin, Ursula K",
		}},
		Series:      &model.SeriesRef{Name: "Hainish Cycle", Index: "1"},
		Language:    "en",
		Pubdate:     "1969-03-01",
		Description: desc,
		Identifiers: map[string]string{
			"isbn": "978-0441478125",
			"uuid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
	}
	if withCover {
		bib.CoverPath = "OEBPS/cover.jpg"
	}
	libPath := "Le Guin, Ursula K/The Left Hand of Darkness (1042)"
	epubName := "The Left Hand of Darkness - Ursula K. Le Guin.epub"
	return testutil.WrapBook(bookmodel.NewBook(bib, bookmodel.Meta{ID: id, Status: "unread", Tags: []string{"sci-fi", "classic", "feminist"}}, bookmodel.Location{
		EpubPath: filepath.Join(libPath, epubName),
	}))
}

// settleGC runs enough GC cycles to drive HeapAlloc to a stable floor. One
// runtime.GC() sweeps, but finalizers and mcache flushing can leave a tail of
// not-yet-reclaimable bytes; looping until the number stops moving is the only
// way to get a reproducible baseline.
func settleGC() uint64 {
	var prev, cur runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&prev)
	for range 10 {
		runtime.GC()
		runtime.ReadMemStats(&cur)
		if cur.HeapAlloc == prev.HeapAlloc {
			return cur.HeapAlloc
		}
		prev = cur
	}
	return cur.HeapAlloc
}

// measureBooksOnly returns the per-book heap cost of building n books with no
// 9P tree, measured as an independent experiment from a settled baseline.
func measureBooksOnly(n int, withCover bool) float64 {
	settleGC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	books := make([]*library.Book, 0, n)
	for i := int64(0); i < int64(n); i++ {
		books = append(books, representativeBook(i, withCover))
	}

	after := settleGC()
	runtime.KeepAlive(books)
	return float64(int64(after)-int64(before.HeapAlloc)) / float64(n)
}

// measureBooksWithDirs returns the per-book heap cost of building n books AND
// n BookDirs wrapping them. Like measureBooksOnly, measured independently from
// a settled baseline so the delta between the two functions is the BookDir
// tree cost, uncontaminated by garbage from the other phase.
func measureBooksWithDirs(n int, withCover bool) float64 {
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	noEdit := func(int64, model.Edits) error { return nil }

	settleGC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	dirs := make([]*BookDir, 0, n)
	for i := int64(0); i < int64(n); i++ {
		b := representativeBook(i, withCover)
		dirs = append(dirs, NewBookDir(f, nil, noEdit, b))
	}

	after := settleGC()
	runtime.KeepAlive(dirs)
	return float64(int64(after)-int64(before.HeapAlloc)) / float64(n)
}

func BenchmarkBookDirMemoryFootprint(b *testing.B) {
	b.Logf("unsafe.Sizeof breakdown (fixed struct size, excludes heap-pointed data):")
	b.Logf("  proto.Qid       = %d B", unsafe.Sizeof(proto.Qid{}))
	b.Logf("  proto.Stat      = %d B", unsafe.Sizeof(proto.Stat{}))
	b.Logf("  fs.StaticDir    = %d B", unsafe.Sizeof(fs.StaticDir{}))
	b.Logf("  fs.BaseFile     = %d B", unsafe.Sizeof(fs.BaseFile{}))
	b.Logf("  fs.StaticFile   = %d B", unsafe.Sizeof(fs.StaticFile{}))
	b.Logf("  fieldFile       = %d B", unsafe.Sizeof(fieldFile{}))
	b.Logf("  epubFile        = %d B", unsafe.Sizeof(epubFile{}))
	b.Logf("  coverFile       = %d B", unsafe.Sizeof(coverFile{}))
	b.Logf("  library.Book      = %d B", unsafe.Sizeof(library.Book{}))
	b.Logf("  BookDir         = %d B", unsafe.Sizeof(BookDir{}))
	b.Logf("")

	for _, n := range []int{100, 1000, 10000} {
		for _, withCover := range []bool{false, true} {
			perBook := measureBooksOnly(n, withCover)
			perTotal := measureBooksWithDirs(n, withCover)
			perDir := perTotal - perBook
			b.Logf("N=%-6d cover=%-5v  per-book=%5.0f B  per-BookDir(marginal)=%5.0f B  total/book=%5.0f B",
				n, withCover, perBook, perDir, perTotal)
		}
	}
	b.Logf("")
	b.Logf("Projections (per-book + per-BookDir), linear extrapolation from N=10000 with cover:")
	perBook := measureBooksOnly(10000, true)
	perTotal := measureBooksWithDirs(10000, true)
	perDir := perTotal - perBook
	b.Logf("  baseline: per-book=%.0f B  per-BookDir(marginal)=%.0f B  total/book=%.0f B",
		perBook, perDir, perTotal)
	for _, k := range []int{1000, 5000, 10000, 50000, 100000} {
		b.Logf("  %6d books  →  %7.1f MiB  (%5.0f B/book × %d)",
			k, float64(k)*perTotal/1024/1024, perTotal, k)
	}
}
