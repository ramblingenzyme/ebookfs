package fs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
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

func bibFields(book *model.Book) map[string]func() string {
	return map[string]func() string{
		"title":       func() string { return book.Title },
		"language":    func() string { return book.Language },
		"pubdate":     func() string { return book.Pubdate },
		"description": func() string { return book.Description },
		"authors": func() string {
			names := make([]string, len(book.Authors))
			for i, a := range book.Authors {
				names[i] = a.Name
			}
			return strings.Join(names, "\n")
		},
		"series": func() string {
			if book.Series == nil {
				return ""
			}
			return book.Series.Name
		},
		"series_index": func() string {
			if book.Series == nil {
				return ""
			}
			return strconv.FormatFloat(book.Series.Index, 'f', -1, 64)
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

	for name, get := range bibFields(book) {
		addReadOnlyField(d, f, name, get)
	}

	// Cover image — only present when the epub declares one.
	if book.CoverPath != "" {
		d.StaticDir.AddChild(newCoverFile(
			f.NewStat("cover.jpg", "glenda", "glenda", 0444),
			lib,
			book,
		))
	}

	return d
}
