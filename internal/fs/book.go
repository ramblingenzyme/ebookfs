package fs

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

func addReadOnlyField(d *bookDir, f *fs.FS, name string, get func() string) {
	d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0444), get, nil))
}

type bookDir struct {
	fs.StaticDir
	*model.Book
}

func metaFields(book *model.Book, lib *library.Library) map[string]struct {
	get func() string
	set func(string) error
} {
	saveMeta := func(set func(string) error) func(string) error {
		return func(s string) error {
			if err := set(s); err != nil {
				return err
			}
			book.Meta.DateModified = time.Now()
			return lib.WriteMeta(book)
		}
	}

	return map[string]struct {
		get func() string
		set func(string) error
	}{
		"status": {
			func() string { return book.Meta.Status },
			saveMeta(func(s string) error {
				switch s {
				case "unread", "reading", "read", "abandoned":
					book.Meta.Status = s
					return nil
				default:
					return fmt.Errorf("invalid status %q: must be unread, reading, read, or abandoned", s)
				}
			}),
		},
		// TODO: rating should be a float32 0–5 (e.g. 4.75); update model, schema, and this validation together.
		"rating": {
			func() string { return strconv.Itoa(book.Meta.Rating) },
			saveMeta(func(s string) error {
				n, err := strconv.Atoi(s)
				if err != nil || n < 0 || n > 5 {
					return fmt.Errorf("invalid rating %q: must be an integer 0-5", s)
				}
				book.Meta.Rating = n
				return nil
			}),
		},
		"tags": {
			func() string { return strings.Join(book.Meta.Tags, "\n") },
			saveMeta(func(s string) error {
				book.Meta.Tags = strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
				return nil
			}),
		},
	}
}

func bibFields(book *model.Book) map[string]string {
	authorNames := make([]string, len(book.Authors))
	for i, a := range book.Authors {
		authorNames[i] = a.Name
	}
	series, seriesIndex := "", ""
	if book.Series != nil {
		series = book.Series.Name
		seriesIndex = strconv.FormatFloat(book.Series.Index, 'f', -1, 64)
	}
	return map[string]string{
		"title":        book.Title,
		"authors":      strings.Join(authorNames, "\n"),
		"language":     book.Language,
		"pubdate":      book.Pubdate,
		"description":  book.Description,
		"series":       series,
		"series_index": seriesIndex,
	}
}

func newBookDir(f *fs.FS, lib *library.Library, book *model.Book) *bookDir {
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

	for name, fld := range metaFields(book, lib) {
		d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0644), fld.get, fld.set))
	}

	for name, val := range bibFields(book) {
		addReadOnlyField(d, f, name, func() string { return val })
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
