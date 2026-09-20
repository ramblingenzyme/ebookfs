package vfile

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ReadAtFile is a base for files that hold one EpubReader per fid, acquired
// via an injected open func. Read delegates to ReadAt and swallows io.EOF
// (the reader may return its final bytes and EOF in a single call).
type ReadAtFile struct {
	fs.BaseFile
	open func() (library.EpubReader, error)
	fids map[uint64]library.EpubReader
}

func NewReadAtFile(stat *proto.Stat, open func() (library.EpubReader, error)) ReadAtFile {
	return ReadAtFile{
		BaseFile: *fs.NewBaseFile(stat),
		open:     open,
		fids:     make(map[uint64]library.EpubReader),
	}
}

func (f *ReadAtFile) Open(fid uint64, _ proto.Mode) error {
	r, err := f.open()
	if err != nil {
		return err
	}
	f.Lock()
	f.fids[fid] = r
	f.Unlock()
	return nil
}

func (f *ReadAtFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	// Copy the reader out and release the lock before the disk read: holding
	// even the read lock across ReadAt would let one slow read plus a queued
	// Open (write lock) stall every other in-flight read of this file. The
	// reader is per-fid and a client never reads a fid it is clunking, so the
	// unlocked ReadAt cannot race its own Close.
	f.RLock()
	r := f.fids[fid]
	f.RUnlock()
	if r == nil {
		return nil, errors.New("not open")
	}
	buf := make([]byte, count)
	n, err := r.ReadAt(buf, int64(offset))
	if err == io.EOF {
		err = nil
	}
	return buf[:n], err
}

func (f *ReadAtFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	if r, ok := f.fids[fid]; ok {
		r.Close()
		delete(f.fids, fid)
	}
	return nil
}
