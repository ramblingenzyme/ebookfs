package fs

import (
	"errors"
	"io"
	"log"
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ReaderConfig is the frontend's view of the reader/ settings. main builds it
// from config so the frontend stays decoupled from the config package.
type ReaderConfig struct {
	Statuses []string
	Convert  bool
}

// readerDir is the reader/ export view: the books whose status is in the
// configured set, grouped by author, served through the injected Exporter. It
// mirrors byAuthorDir, but its leaves are export files rather than bookDirs and
// it files each book under a single folder named for all its authors — so a
// co-authored book is exported once, not duplicated under each author.
type readerDir struct {
	groupingDir
	exp      library.Exporter
	included map[string]bool
	warmer   *warmer // nil unless conversion is enabled
}

func newReaderDir(reg *bookRegistry, exp library.Exporter, cfg ReaderConfig) *readerDir {
	included := make(map[string]bool, len(cfg.Statuses))
	for _, s := range cfg.Statuses {
		included[s] = true
	}
	d := &readerDir{
		groupingDir: newGroupingDir(reg.f, "reader"),
		exp:         exp,
		included:    included,
	}
	if cfg.Convert {
		d.warmer = newWarmer(exp)
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

// authorName is the FAT-safe folder name for a book's authors, joined so a
// co-authored book lands in one folder; the result survives an rsync onto a FAT
// device.
func (d *readerDir) authorName(b *model.Book) string {
	var names []string
	for _, a := range b.Authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	name := "Unknown"
	if len(names) > 0 {
		name = strings.Join(names, " & ")
	}
	if fat, err := library.ForFAT(name); err == nil {
		name = fat
	}
	return name
}

func (d *readerDir) add(dir *bookDir) {
	if !d.included[dir.Book.Meta.Status] {
		return
	}
	ad := d.authorDir(d.authorName(dir.Book))
	stat := d.f.NewStat(d.exp.Filename(dir.Book), "glenda", "glenda", 0444)
	ad.AddChild(newReaderFile(stat, d.exp, dir.Book))
	if d.warmer != nil {
		d.warmer.warm(dir.Book)
	}
}

func (d *readerDir) remove(dir *bookDir) {
	if !d.included[dir.Book.Meta.Status] {
		return
	}
	name := d.authorName(dir.Book)
	if child, ok := d.Children()[name]; ok {
		child.(fs.ModDir).DeleteChild(d.exp.Filename(dir.Book))
		d.pruneEmpty(name)
	}
}

// readerFile serves a book's export rendition through the Exporter, holding one
// reader per fid. It mirrors epubFile, but its size is reported live from the
// exporter so a kepub's length appears once its cache is warm.
type readerFile struct {
	fs.BaseFile
	exp  library.Exporter
	book *model.Book
	fids map[uint64]library.EpubReader
}

func newReaderFile(stat *proto.Stat, exp library.Exporter, book *model.Book) *readerFile {
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
	if size, ok := r.exp.Size(r.book); ok {
		s.Length = uint64(size)
	}
	return s
}

func (r *readerFile) Open(fid uint64, omode proto.Mode) error {
	rd, err := r.exp.Open(r.book)
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

// warmer converts kepubs off the read path: when a book enters the reader set,
// the view enqueues it here so its cache is built before the next rsync. Enqueue
// is non-blocking because add runs under the registry lock; a full queue drops
// the warm and the read path (Exporter.Open) converts on demand instead.
type warmer struct {
	exp library.Exporter
	ch  chan *model.Book
}

func newWarmer(exp library.Exporter) *warmer {
	// The buffer holds book pointers, so it's cheap to size generously; it mainly
	// needs to absorb the initial-population burst (one warm per eligible book)
	// without dropping. Beyond it, warm() falls back to the lazy read path.
	w := &warmer{exp: exp, ch: make(chan *model.Book, 4096)}
	for i := 0; i < 4; i++ {
		go w.run()
	}
	return w
}

func (w *warmer) warm(b *model.Book) {
	select {
	case w.ch <- b:
	default: // queue full: skip the proactive warm, the read path still converts
	}
}

func (w *warmer) run() {
	for b := range w.ch {
		if err := w.exp.Ensure(b); err != nil {
			log.Printf("reader: warm kepub for book %d: %v", b.Meta.ID, err)
		}
	}
}
