package fs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/backend/epub"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func addReadOnlyField(d *bookDir, f *fs.FS, name string, get func() string) {
	d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0444), get, nil))
}

type bookDir struct {
	fs.StaticDir
	*model.Book
}

// Stat reports the book's title as the entry name, recomputed live so a title
// edit shows up wherever the bare bookDir is listed (all-books, by-author). The
// Qid stays fixed, so a client with the epub open keeps its handle across a rename.
func (d *bookDir) Stat() proto.Stat {
	s := d.StaticDir.Stat()
	s.Name = d.Book.Title
	return s
}

type metaField struct {
	get func(*model.Book) string
	set func(*model.Book, string) error
}

type bibField struct {
	get   func(*model.Book) string
	edits func(*model.Book, string) (epub.Edits, error)
}

func metaFields() map[string]metaField {
	return map[string]metaField{
		"status": {
			func(b *model.Book) string { return b.Meta.Status },
			func(b *model.Book, s string) error {
				switch s {
				case "unread", "reading", "read", "abandoned":
					b.Meta.Status = s
					return nil
				default:
					return fmt.Errorf("invalid status %q: must be unread, reading, read, or abandoned", s)
				}
			},
		},
		// TODO: rating should be a float32 0–5 (e.g. 4.75); update model, schema, and this validation together.
		"rating": {
			func(b *model.Book) string { return strconv.Itoa(b.Meta.Rating) },
			func(b *model.Book, s string) error {
				n, err := strconv.Atoi(s)
				if err != nil || n < 0 || n > 5 {
					return fmt.Errorf("invalid rating %q: must be an integer 0-5", s)
				}
				b.Meta.Rating = n
				return nil
			},
		},
		"tags": {
			func(b *model.Book) string { return strings.Join(b.Meta.Tags, "\n") },
			func(b *model.Book, s string) error {
				b.Meta.Tags = strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
				return nil
			},
		},
	}
}

func bibFields() map[string]bibField {
	return map[string]bibField{
		"title": {
			get: func(b *model.Book) string { return b.Title },
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				if strings.TrimSpace(s) == "" {
					return epub.Edits{}, fmt.Errorf("title must not be empty")
				}
				return epub.Edits{Title: &s}, nil
			},
		},
		"language": {
			get: func(b *model.Book) string { return b.Language },
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				return epub.Edits{Language: &s}, nil
			},
		},
		"description": {
			get: func(b *model.Book) string { return b.Description },
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				return epub.Edits{Description: &s}, nil
			},
		},
		"authors": {
			get: func(b *model.Book) string {
				names := make([]string, len(b.Authors))
				for i, a := range b.Authors {
					names[i] = a.Name
				}
				return strings.Join(names, "\n")
			},
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				var names []string
				for _, n := range strings.Split(s, "\n") {
					if n = strings.TrimSpace(n); n != "" {
						names = append(names, n)
					}
				}
				if len(names) == 0 {
					return epub.Edits{}, fmt.Errorf("at least one author is required")
				}
				authors := make([]epub.Author, len(names))
				for i, n := range names {
					authors[i] = epub.Author{Name: n}
				}
				return epub.Edits{Authors: &authors}, nil
			},
		},
		"series": {
			get: func(b *model.Book) string {
				if b.Series == nil {
					return ""
				}
				return b.Series.Name
			},
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				return epub.Edits{Series: &s}, nil
			},
		},
		"series_index": {
			get: func(b *model.Book) string {
				if b.Series == nil {
					return ""
				}
				return strconv.FormatFloat(b.Series.Index, 'f', 1, 64)
			},
			edits: func(b *model.Book, s string) (epub.Edits, error) {
				if b.Series == nil {
					return epub.Edits{}, fmt.Errorf("book has no series to set an index on")
				}
				idx, err := strconv.ParseFloat(s, 64)
				if err != nil {
					return epub.Edits{}, fmt.Errorf("invalid series index %q", s)
				}
				idx = math.Round(idx*10) / 10
				name := b.Series.Name
				return epub.Edits{Series: &name, SeriesIndex: &idx}, nil
			},
		},
	}
}

func newBookDir(reg *bookRegistry, book *model.Book) *bookDir {
	f, lib := reg.f, reg.lib
	d := &bookDir{
		StaticDir: *fs.NewStaticDir(f.NewStat(book.Title, "glenda", "glenda", 0755|proto.DMDIR)),
		Book:      book,
	}

	d.StaticDir.AddChild(fs.NewStaticFile(
		f.NewStat("id", "glenda", "glenda", 0444),
		fmt.Appendf(nil, "%d\n", book.Meta.ID),
	))

	d.StaticDir.AddChild(newEpubFile(
		f.NewStat(book.EpubFilename, "glenda", "glenda", 0444),
		lib,
		book,
	))

	// Meta writes route through the registry so the change is validated,
	// persisted, and bracketed by view remove/add (rehoming if a meta field
	// drives grouping). get reads the live book; set mutates a validated copy.
	for name, fld := range metaFields() {
		get := func() string { return fld.get(d.Book) }
		set := func(s string) error {
			return reg.editMeta(d.Book.Meta.ID, func(b *model.Book) error {
				return fld.set(b, s)
			})
		}
		d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0644), get, set))
	}

	// Bib fields — writable through the registry.
	for name, fld := range bibFields() {
		get := func() string { return fld.get(d.Book) }
		set := func(s string) error {
			edits, err := fld.edits(d.Book, s)
			if err != nil {
				return err
			}
			return reg.editBib(d.Book.Meta.ID, edits)
		}
		d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0644), get, set))
	}

	// Read-only bib fields.
	addReadOnlyField(d, f, "pubdate", func() string { return book.Pubdate })

	// Cover image — only present when the epub declares one.
	if book.CoverPath != "" {
		d.StaticDir.AddChild(newCoverFile(
			f.NewStat("cover.jpg", "glenda", "glenda", 0644),
			lib,
			book,
		))
	}

	return d
}
