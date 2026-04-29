package node

import (
	"bytes"
	"context"
	"fmt"
	"sync/atomic"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

var importQIDCounter uint64 = 1000000

func nextImportQID() uint64 {
	return atomic.AddUint64(&importQIDCounter, 1)
}

type ImportDir struct {
	templatefs.NoopFile
	qid    p9.QID
	logQID p9.QID
	lib    library.Library
	log    *StreamLog
}

func newImportDir(lib library.Library, log *StreamLog, qid, logQID p9.QID) *ImportDir {
	return &ImportDir{qid: qid, logQID: logQID, lib: lib, log: log}
}

func (d *ImportDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, d, nil
	}
	if names[0] == "log" {
		if len(names) > 1 {
			return nil, nil, syscall.ENOTDIR
		}
		return []p9.QID{d.logQID}, &LogFile{qid: d.logQID, log: d.log, done: make(chan struct{})}, nil
	}
	return nil, nil, syscall.ENOENT
}

func (d *ImportDir) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return d.qid,
		p9.AttrMask{Mode: true, NLink: true},
		p9.Attr{Mode: p9.ModeDirectory | 0755, NLink: 2},
		nil
}

func (d *ImportDir) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return d.qid, 4096, nil
}

func (d *ImportDir) Create(name string, flags p9.OpenFlags, permissions p9.FileMode, uid p9.UID, gid p9.GID) (p9.File, p9.QID, uint32, error) {
	qid := p9.QID{Type: p9.TypeRegular, Path: nextImportQID()}
	return &importFile{qid: qid, name: name, lib: d.lib, log: d.log}, qid, 4096, nil
}

func (d *ImportDir) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	all := p9.Dirents{
		{QID: d.logQID, Offset: 1, Type: p9.TypeRegular, Name: "log"},
	}
	if offset >= uint64(len(all)) {
		return nil, nil
	}
	return all[offset:], nil
}

// importFile buffers incoming writes and imports the epub into the library on Close.
type importFile struct {
	templatefs.NoopFile
	qid  p9.QID
	name string
	buf  bytes.Buffer
	lib  library.Library
	log  *StreamLog
}

func (f *importFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, f, nil
	}
	return nil, nil, syscall.ENOTDIR
}

func (f *importFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return f.qid,
		p9.AttrMask{Mode: true},
		p9.Attr{Mode: p9.ModeRegular | 0200},
		nil
}

func (f *importFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return f.qid, 4096, nil
}

func (f *importFile) WriteAt(buf []byte, offset int64) (int, error) {
	return f.buf.Write(buf)
}

func (f *importFile) Close() error {
	book, err := f.lib.AddBook(context.Background(), &f.buf)
	if err != nil {
		f.log.Append(fmt.Sprintf("ERR %s — %s", f.name, err))
		return nil
	}
	f.log.Append(fmt.Sprintf("OK  %s — %s", f.name, book.Title))
	return nil
}
