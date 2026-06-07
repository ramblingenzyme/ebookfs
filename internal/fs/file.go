package fs

import (
	"errors"
	"io"
	"os"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// epubFile serves the epub from disk, holding one OS file handle per fid.
// Size is snapshotted at construction; content is read on demand via ReadAt.
type epubFile struct {
	fs.BaseFile
	path string
	fids map[uint64]*os.File
}

func newEpubFile(stat *proto.Stat, path string) *epubFile {
	if info, err := os.Stat(path); err == nil {
		stat.Length = uint64(info.Size())
	}
	return &epubFile{
		BaseFile: *fs.NewBaseFile(stat),
		path:     path,
		fids:     make(map[uint64]*os.File),
	}
}

func (e *epubFile) Open(fid uint64, omode proto.Mode) error {
	f, err := os.Open(e.path)
	if err != nil {
		return err
	}
	e.Lock()
	e.fids[fid] = f
	e.Unlock()
	return nil
}

func (e *epubFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	e.RLock()
	f := e.fids[fid]
	e.RUnlock()
	if f == nil {
		return nil, errors.New("not open")
	}
	buf := make([]byte, count)
	n, err := f.ReadAt(buf, int64(offset))
	if err == io.EOF {
		err = nil
	}
	return buf[:n], err
}

func (e *epubFile) Close(fid uint64) error {
	e.Lock()
	defer e.Unlock()
	if f, ok := e.fids[fid]; ok {
		f.Close()
		delete(e.fids, fid)
	}
	return nil
}
