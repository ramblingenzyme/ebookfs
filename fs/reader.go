package fs

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// readerDir is the reader/ export view: the books whose status is in the
// configured set, grouped by author, served through the injected Exporter. It
// mirrors byAuthorDir, but its leaves are export files rather than bookDirs and
// it files each book under a single folder named for all its authors — so a
// co-authored book is exported once, not duplicated under each author.
type readerDir struct {
	groupingDir
	exp      library.Exporter
	included map[string]bool
}

func newReaderDir(reg *bookRegistry, exp library.Exporter) *readerDir {
	included := make(map[string]bool)
	for _, s := range exp.Statuses() {
		included[s] = true
	}
	d := &readerDir{
		groupingDir: newGroupingDir(reg.f, "reader"),
		exp:         exp,
		included:    included,
	}
	reg.AddView(d)
	return d
}

// authorDir returns the subdir for an author name, creating it on first use.
func (d *readerDir) authorDir(name string) fs.ModDir {
	if child, ok := d.Children()[name]; ok {
		return child.(fs.ModDir)
	}
	ad := fs.NewStaticDir(d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR))
	d.StaticDir.AddChild(ad)
	return ad
}

func (d *readerDir) add(dir *bookDir) {
	b := dir.Book()
	if !d.included[b.Meta.Status] {
		return
	}
	ad := d.authorDir(d.exp.Dirname(b))
	stat := d.f.NewStat(d.exp.Filename(b), "glenda", "glenda", 0444)
	ad.AddChild(newReaderFile(stat, d.exp, dir.Book))
	d.exp.Warm(b)
}

func (d *readerDir) remove(dir *bookDir) {
	b := dir.Book()
	if !d.included[b.Meta.Status] {
		return
	}
	name := d.exp.Dirname(b)
	if child, ok := d.Children()[name]; ok {
		child.(fs.ModDir).DeleteChild(d.exp.Filename(b))
		d.pruneEmpty(name)
	}
}

// readerFile serves a book's export rendition through the Exporter, holding one
// reader per fid. It mirrors epubFile, but its size is reported live from the
// exporter so a kepub's length appears once its cache is warm.
type readerFile struct {
	fs.BaseFile
	exp  library.Exporter
	book func() *model.Book
	fids map[uint64]library.EpubReader
}

func newReaderFile(stat *proto.Stat, exp library.Exporter, book func() *model.Book) *readerFile {
	return &readerFile{
		BaseFile: *fs.NewBaseFile(stat),
		exp:      exp,
		book:     book,
		fids:     make(map[uint64]library.EpubReader),
	}
}

// Stat reports the export size when known (cheap; never triggers a conversion),
// so a cold kepub lists as length 0 until its cache is warm.
func (r *readerFile) Stat() proto.Stat {
	s := r.BaseFile.Stat()
	if size, ok := r.exp.Size(r.book()); ok {
		s.Length = uint64(size)
	}
	return s
}

func (r *readerFile) Open(fid uint64, omode proto.Mode) error {
	rd, err := r.exp.Open(r.book())
	if err != nil {
		return err
	}
	r.Lock()
	r.fids[fid] = rd
	r.Unlock()
	return nil
}

func (r *readerFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	r.RLock()
	defer r.RUnlock()
	rd := r.fids[fid]
	if rd == nil {
		return nil, errors.New("not open")
	}
	buf := make([]byte, count)
	n, err := rd.ReadAt(buf, int64(offset))
	if err == io.EOF {
		err = nil
	}
	return buf[:n], err
}

func (r *readerFile) Close(fid uint64) error {
	r.Lock()
	defer r.Unlock()
	if rd, ok := r.fids[fid]; ok {
		rd.Close()
		delete(r.fids, fid)
	}
	return nil
}


