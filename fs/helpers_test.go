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
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
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
	editFn         func(int64, model.Edits) (*model.Book, error)
	ingestFn       func(string) (*model.Book, error)
	createIngestFn func() (library.IngestHandle, error)
	extractCoverFn func(int64) ([]byte, error)
	extractOPFFn   func(int64) ([]byte, error)
	openEpubFn     func(int64) (library.EpubReader, error)
	queryFn        func(model.Filter) ([]*model.Book, error)
	reindexFn      func() error
	deleteFn       func(int64) error
}

func (l fakeLib) Close() error                                             { return nil }
func (l fakeLib) Exporter(_ config.ReaderConfig) (library.Exporter, error) { return nil, nil }
func (l fakeLib) Edit(id int64, e model.Edits) (*model.Book, error) {
	if l.editFn != nil {
		return l.editFn(id, e)
	}
	return nil, errors.New("fakeLib: no editFn")
}
func (l fakeLib) CreateIngest() (library.IngestHandle, error) {
	if l.createIngestFn != nil {
		return l.createIngestFn()
	}
	return fakeIngestHandle{ingestFn: l.ingestFn}, nil
}
func (l fakeLib) ExtractCover(id int64) ([]byte, error) {
	if l.extractCoverFn != nil {
		return l.extractCoverFn(id)
	}
	return nil, nil
}
func (l fakeLib) ExtractOPF(id int64) ([]byte, error) {
	if l.extractOPFFn != nil {
		return l.extractOPFFn(id)
	}
	return nil, nil
}
func (l fakeLib) OpenEpub(id int64) (library.EpubReader, error) {
	if l.openEpubFn != nil {
		return l.openEpubFn(id)
	}
	return nil, nil
}

// fakeIngestHandle is a test implementation of library.IngestHandle. Inbox tests
// never read back written bytes, so WriteAt is a no-op that reports acceptance;
// Ingest delegates to the injected ingestFn (nil ingestFn yields a nil book).
type fakeIngestHandle struct {
	ingestFn func(string) (*model.Book, error)
}

func (h fakeIngestHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (h fakeIngestHandle) Ingest() (*model.Book, error) {
	if h.ingestFn == nil {
		return nil, nil
	}
	return h.ingestFn("")
}
func (l fakeLib) Query(f model.Filter) ([]*model.Book, error) {
	if l.queryFn != nil {
		return l.queryFn(f)
	}
	return nil, nil
}
func (l fakeLib) Reindex() error {
	if l.reindexFn != nil {
		return l.reindexFn()
	}
	return nil
}
func (l fakeLib) Delete(id int64) error {
	if l.deleteFn != nil {
		return l.deleteFn(id)
	}
	return nil
}

var _ library.Library = (fakeLib{})

// fixed returns a book getter that always yields b, standing in for
// bookDir.Book in tests that construct child files directly.
func fixed(b *model.Book) func() *model.Book {
	return func() *model.Book { return b }
}

var makeBook = testutil.MakeBook

func newTestFS(t *testing.T) *fs.FS {
	t.Helper()
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f
}

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library. Tests that also need the FS directly can reach it via reg.f.
func newTestRegistry(t *testing.T) *bookRegistry {
	t.Helper()
	return newBookRegistry(newTestFS(t), nil)
}

type testExporter struct {
	statuses   []string
	openFn     func(*model.Book) (library.EpubReader, error)
	sizeFn     func(*model.Book) (int64, bool)
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
func (e testExporter) Warm(*model.Book) {}
func (e testExporter) Filename(b *model.Book) string {
	if e.filenameFn != nil {
		return e.filenameFn(b)
	}
	return b.EpubFilename
}
func (e testExporter) Dirname(b *model.Book) string {
	var names []string
	for _, a := range b.Authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return "Unknown"
	}
	return strings.Join(names, " & ")
}
func (e testExporter) Statuses() []string { return e.statuses }

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
