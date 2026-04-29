package node

import (
	"io"
	"sync"
	"syscall"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type LogFile struct {
	templatefs.NoopFile
	qid       p9.QID
	log       *StreamLog
	done      chan struct{}
	closeOnce sync.Once
}

func (f *LogFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, &LogFile{qid: f.qid, log: f.log, done: make(chan struct{})}, nil
	}
	return nil, nil, syscall.ENOTDIR
}

func (f *LogFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return f.qid,
		p9.AttrMask{Mode: true},
		p9.Attr{Mode: p9.ModeRegular | 0444},
		nil
}

func (f *LogFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return f.qid, 4096, nil
}

func (f *LogFile) ReadAt(buf []byte, offset int64) (int, error) {
	n, err := f.log.ReadAt(buf, offset, f.done)
	if err == io.EOF {
		return 0, nil
	}
	return n, err
}

func (f *LogFile) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}
