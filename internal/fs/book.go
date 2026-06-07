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

type bookDir struct {
	fs.StaticDir
	*model.Book
}

func newBookDir(f *fs.FS, lib *library.Library, book *model.Book) *bookDir {
	d := &bookDir{
		StaticDir: *fs.NewStaticDir(f.NewStat(book.Title, "glenda", "glenda", 0755|proto.DMDIR)),
		Book:      book,
	}

	addField := func(name string, get func() string, set func(string) error) {
		d.StaticDir.AddChild(newFieldFile(f.NewStat(name, "glenda", "glenda", 0644), get, set))
	}

	// saveMeta wraps a setter: if set succeeds, stamps DateModified and persists.
	saveMeta := func(set func(string) error) func(string) error {
		return func(s string) error {
			if err := set(s); err != nil {
				return err
			}
			book.Meta.DateModified = time.Now()
			return lib.WriteMeta(book)
		}
	}

	d.StaticDir.AddChild(fs.NewStaticFile(
		f.NewStat("id", "glenda", "glenda", 0444),
		fmt.Appendf(nil, "%d\n", book.Meta.ID),
	))

	addField("status",
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
	)

	addField("rating",
		func() string { return strconv.Itoa(book.Meta.Rating) },
		saveMeta(func(s string) error {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 || n > 5 {
				return fmt.Errorf("invalid rating %q: must be an integer 0-5", s)
			}
			book.Meta.Rating = n
			return nil
		}),
	)

	addField("tags",
		func() string { return strings.Join(book.Meta.Tags, "\n") },
		saveMeta(func(s string) error {
			book.Meta.Tags = strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
			return nil
		}),
	)

	d.StaticDir.AddChild(newEpubFile(
		f.NewStat(book.EpubFilename, "glenda", "glenda", 0444),
		lib.EpubPath(book),
	))

	return d
}
