package fs

// Untested:
//   - StartServer() — calls go9p.Serve which blocks; wiring is verified
//     via setupServer() instead, which covers all setup logic.
//   - inboxCreateFile closure AddChild error branch — AddChild with a fresh
//     name on a correctly-configured dir always succeeds.
//   - inboxFile.Close edge-case branches (parent not a ModDir, etc.) —
//     not worth the test complexity.
//   - newBookDir one line inside the for-range loop over fields (88.9%).
//   - A few single-line branches in inboxCreateFile, inboxFile.Close,
//     registry.edit — each one statement wide and uninteresting.

import (
	"bytes"
	"io"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

type fakeEpubReader struct {
	*bytes.Reader
	closed bool
}

func (r *fakeEpubReader) Close() error {
	r.closed = true
	return nil
}

var _ library.EpubReader = (*fakeEpubReader)(nil)
var _ io.ReaderAt = (*fakeEpubReader)(nil)

type fakeLib struct {
	editFn         func(*model.Book, model.Edits) (*model.Book, error)
	ingestFn       func(string) (*model.Book, error)
	extractCoverFn func(*model.Book) ([]byte, error)
	extractOPFFn   func(*model.Book) ([]byte, error)
	writeCoverFn   func(*model.Book, []byte) error
	openEpubFn     func(*model.Book) (library.EpubReader, error)
	listAllFn      func() ([]*model.Book, error)
	reindexFn      func() error
	deleteFn       func(*model.Book) error
}

func (l fakeLib) Edit(b *model.Book, e model.Edits) (*model.Book, error) {
	if l.editFn != nil {
		return l.editFn(b, e)
	}
	return b, nil
}
func (l fakeLib) Ingest(path string) (*model.Book, error) {
	if l.ingestFn != nil {
		return l.ingestFn(path)
	}
	return nil, nil
}
func (l fakeLib) ExtractCover(b *model.Book) ([]byte, error) {
	if l.extractCoverFn != nil {
		return l.extractCoverFn(b)
	}
	return nil, nil
}
func (l fakeLib) ExtractOPF(b *model.Book) ([]byte, error) {
	if l.extractOPFFn != nil {
		return l.extractOPFFn(b)
	}
	return nil, nil
}
func (l fakeLib) WriteCover(b *model.Book, data []byte) error {
	if l.writeCoverFn != nil {
		return l.writeCoverFn(b, data)
	}
	return nil
}
func (l fakeLib) OpenEpub(b *model.Book) (library.EpubReader, error) {
	if l.openEpubFn != nil {
		return l.openEpubFn(b)
	}
	return nil, nil
}
func (l fakeLib) ListAll() ([]*model.Book, error) {
	if l.listAllFn != nil {
		return l.listAllFn()
	}
	return nil, nil
}
func (l fakeLib) Reindex() error {
	if l.reindexFn != nil {
		return l.reindexFn()
	}
	return nil
}
func (l fakeLib) Delete(b *model.Book) error {
	if l.deleteFn != nil {
		return l.deleteFn(b)
	}
	return nil
}

var _ library.Library = (fakeLib{})

func makeBook(id int64, title string, authors ...string) *model.Book {
	auths := make([]model.Author, len(authors))
	for i, name := range authors {
		auths[i] = model.Author{Name: name}
	}
	return model.NewBook(
		model.Bib{Title: title, Authors: auths},
		model.Meta{ID: id},
		model.Location{},
	)
}

func newTestFS(t *testing.T) *fs.FS {
	t.Helper()
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f
}

func newTestRegistry(t *testing.T, f *fs.FS) *bookRegistry {
	t.Helper()
	return newBookRegistry(f, nil)
}

type testExporter struct {
	openFn     func(*model.Book) (library.EpubReader, error)
	sizeFn     func(*model.Book) (int64, bool)
	ensureFn   func(*model.Book) error
	filenameFn func(*model.Book) string
}

func (e testExporter) Open(b *model.Book) (library.EpubReader, error) {
	if e.openFn != nil {
		return e.openFn(b)
	}
	return nil, nil
}
func (e testExporter) Size(b *model.Book) (int64, bool) {
	if e.sizeFn != nil {
		return e.sizeFn(b)
	}
	return 0, false
}
func (e testExporter) Ensure(b *model.Book) error {
	if e.ensureFn != nil {
		return e.ensureFn(b)
	}
	return nil
}
func (e testExporter) Filename(b *model.Book) string {
	if e.filenameFn != nil {
		return e.filenameFn(b)
	}
	return b.EpubFilename
}

func dirChildNames(d fs.Dir) []string {
	var names []string
	for name := range d.Children() {
		names = append(names, name)
	}
	return names
}

func firstDirChildNames(d fs.Dir) []string {
	for _, child := range d.Children() {
		if dir, ok := child.(fs.Dir); ok {
			return dirChildNames(dir)
		}
	}
	return nil
}

func protoDir(name string) *proto.Stat {
	return &proto.Stat{Name: name}
}
