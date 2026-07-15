// Package libfake provides test doubles for the library facade interfaces
// (library.Library, IngestHandle, EpubReader, Exporter), shared by the fs
// frontend package tests.
//
// It is kept apart from internal/testutil because it imports library: several
// of library's own internal packages have white-box tests that import
// testutil, so pulling library into testutil would create a test-time import
// cycle. Packages under fs/ already depend on library, so importing libfake
// from their tests is cycle-free.
package libfake

import (
	"bytes"
	"errors"
	"io"
	"slices"

	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubReader is a fake library.EpubReader over an in-memory buffer. Closed
// records whether Close has been called.
type EpubReader struct {
	*bytes.Reader
	Closed bool
}

func (r *EpubReader) Close() error {
	r.Closed = true
	return nil
}

var _ library.EpubReader = (*EpubReader)(nil)
var _ io.ReaderAt = (*EpubReader)(nil)

// Lib is a fake library.Library whose behavior is injected per method; a nil
// hook yields a benign zero result (except Edit, which errors to catch
// unstubbed edit paths).
type Lib struct {
	EditFn         func(int64, model.Edits) (*model.Book, error)
	IngestFn       func(string) (*model.Book, error)
	CreateIngestFn func() (library.IngestHandle, error)
	ExtractCoverFn func(int64) ([]byte, error)
	ExtractOPFFn   func(int64) ([]byte, error)
	OpenEpubFn     func(int64) (library.EpubReader, error)
	QueryFn        func(model.Filter) ([]*model.Book, error)
	SearchFn       func(model.Query) ([]*model.Book, error)
	StatsFn        func() (*model.Stats, error)
	ReindexFn      func() error
	DeleteFn       func(int64) error
}

var _ library.Library = (Lib{})

func (l Lib) Close() error                                             { return nil }
func (l Lib) Exporter(_ config.ReaderConfig) (library.Exporter, error) { return nil, nil }

func (l Lib) Edit(id int64, e model.Edits) (*model.Book, error) {
	if l.EditFn != nil {
		return l.EditFn(id, e)
	}
	return nil, errors.New("libfake.Lib: no EditFn")
}

func (l Lib) CreateIngest() (library.IngestHandle, error) {
	if l.CreateIngestFn != nil {
		return l.CreateIngestFn()
	}
	return IngestHandle{IngestFn: l.IngestFn}, nil
}

func (l Lib) ExtractCover(id int64) ([]byte, error) {
	if l.ExtractCoverFn != nil {
		return l.ExtractCoverFn(id)
	}
	return nil, nil
}

func (l Lib) ExtractOPF(id int64) ([]byte, error) {
	if l.ExtractOPFFn != nil {
		return l.ExtractOPFFn(id)
	}
	return nil, nil
}

func (l Lib) OpenEpub(id int64) (library.EpubReader, error) {
	if l.OpenEpubFn != nil {
		return l.OpenEpubFn(id)
	}
	return nil, nil
}

func (l Lib) Query(f model.Filter) ([]*model.Book, error) {
	if l.QueryFn != nil {
		return l.QueryFn(f)
	}
	return nil, nil
}

func (l Lib) Search(q model.Query) ([]*model.Book, error) {
	if l.SearchFn != nil {
		return l.SearchFn(q)
	}
	return nil, nil
}

func (l Lib) Stats() (*model.Stats, error) {
	if l.StatsFn != nil {
		return l.StatsFn()
	}
	return &model.Stats{}, nil
}

func (l Lib) Reindex() error {
	if l.ReindexFn != nil {
		return l.ReindexFn()
	}
	return nil
}

func (l Lib) Delete(id int64) error {
	if l.DeleteFn != nil {
		return l.DeleteFn(id)
	}
	return nil
}

// IngestHandle is a fake library.IngestHandle. WriteAt accepts and discards
// bytes (tests never read them back); Ingest delegates to IngestFn, with a nil
// hook yielding a nil book.
type IngestHandle struct {
	IngestFn func(string) (*model.Book, error)
}

func (h IngestHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (h IngestHandle) Ingest() (*model.Book, error) {
	if h.IngestFn == nil {
		return nil, nil
	}
	return h.IngestFn("")
}

// Exporter is a fake library.Exporter with injectable behavior. Includes
// reports whether the book's status is in StatusList, mirroring the real
// exporters' status policy.
type Exporter struct {
	StatusList []string
	OpenFn     func(*model.Book) (library.EpubReader, error)
	SizeFn     func(*model.Book) (int64, bool)
	FilenameFn func(*model.Book) string
}

func (e Exporter) Open(b *model.Book) (library.EpubReader, error) {
	if e.OpenFn != nil {
		return e.OpenFn(b)
	}
	return nil, nil
}

func (e Exporter) Size(b *model.Book) (int64, bool) {
	if e.SizeFn != nil {
		return e.SizeFn(b)
	}
	return 0, false
}

func (e Exporter) Warm(*model.Book) {}

func (e Exporter) Filename(b *model.Book) string {
	if e.FilenameFn != nil {
		return e.FilenameFn(b)
	}
	return b.EpubFilename
}

func (e Exporter) Dirname(b *model.Book) string {
	return model.JoinAuthors(b.Authors, " & ")
}

func (e Exporter) Includes(b *model.Book) bool { return slices.Contains(e.StatusList, b.Meta.Status) }
