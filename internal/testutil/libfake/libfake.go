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
	"slices"

	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubReader is a fake model.EpubReader backed by an in-memory buffer. Closed
// records whether Close has been called. OPFFn and CoverFn supply the OPF and
// cover extraction results.
type EpubReader struct {
	*bytes.Reader
	Closed  bool
	OPFFn   func() ([]byte, error)
	CoverFn func() ([]byte, error)
}

func (r *EpubReader) Close() error {
	r.Closed = true
	return nil
}

func (r *EpubReader) OPF() ([]byte, error) {
	if r.OPFFn != nil {
		return r.OPFFn()
	}
	return nil, nil
}

func (r *EpubReader) Cover() ([]byte, error) {
	if r.CoverFn != nil {
		return r.CoverFn()
	}
	return nil, nil
}

var _ model.EpubReader = (*EpubReader)(nil)

// NewEpubReader returns a fake EpubReader serving data as its raw epub bytes
// and opf/cover from the provided functions. When opfFn or coverFn is nil the
// corresponding method returns (nil, nil).
func NewEpubReader(data []byte, opfFn, coverFn func() ([]byte, error)) *EpubReader {
	return &EpubReader{
		Reader:  bytes.NewReader(data),
		OPFFn:   opfFn,
		CoverFn: coverFn,
	}
}

// Lib is a fake library.Library whose behavior is injected per method; a nil
// hook yields a benign zero result (except Edit and Content, which error to
// catch unstubbed edit/read paths).
type Lib struct {
	EditFn         func(int64, library.Edits) (*library.Book, error)
	IngestFn       func(string) (*library.Book, error)
	CreateIngestFn func() (library.IngestHandle, error)
	ContentFn      func(int64) (model.EpubReader, error)
	SearchFn       func(library.Query) ([]*library.Book, error)
	StatsFn        func() (*model.Stats, error)
	ReindexFn      func() error
	DeleteFn       func(int64) error
}

var _ library.Library = (Lib{})

func (l Lib) Close() error                                             { return nil }
func (l Lib) Exporter(_ config.ReaderConfig) (library.Exporter, error) { return nil, nil }

func (l Lib) Edit(id int64, e library.Edits) (*library.Book, error) {
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

func (l Lib) Content(id int64) (model.EpubReader, error) {
	if l.ContentFn != nil {
		return l.ContentFn(id)
	}
	return nil, errors.New("libfake.Lib: no ContentFn")
}

func (l Lib) Search(q library.Query) ([]*library.Book, error) {
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
	IngestFn func(string) (*library.Book, error)
}

func (h IngestHandle) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }
func (h IngestHandle) Ingest() (*library.Book, error) {
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
	OpenFn     func(*library.Book) (model.EpubReader, error)
	SizeFn     func(*library.Book) (int64, bool)
	FilenameFn func(*library.Book) string
}

func (e Exporter) Open(b *library.Book) (model.EpubReader, error) {
	if e.OpenFn != nil {
		return e.OpenFn(b)
	}
	return nil, errors.New("libfake.Exporter: no OpenFn")
}

func (e Exporter) Close() error { return nil }

func (e Exporter) Size(b *library.Book) (int64, bool) {
	if e.SizeFn != nil {
		return e.SizeFn(b)
	}
	return 0, false
}

func (e Exporter) Warm(*library.Book) {}

func (e Exporter) Filename(b *library.Book) string {
	if e.FilenameFn != nil {
		return e.FilenameFn(b)
	}
	return b.Filename()
}

func (e Exporter) Dirname(b *library.Book) string {
	return model.JoinAuthors(b.Authors(), " & ")
}

func (e Exporter) Includes(b *library.Book) bool { return slices.Contains(e.StatusList, b.Status()) }
