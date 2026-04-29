package node

import (
	"os"
	"syscall"

	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type OSFile struct {
	templatefs.NoopFile
	path string
	qid  p9.QID
	f    *os.File
}

func (e *OSFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, &OSFile{path: e.path, qid: e.qid}, nil
	}
	return nil, nil, syscall.ENOTDIR
}

func (e *OSFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	fi, err := os.Stat(e.path)
	if err != nil {
		return p9.QID{}, p9.AttrMask{}, p9.Attr{}, err
	}
	return e.qid,
		p9.AttrMask{Mode: true, Size: true},
		p9.Attr{Mode: p9.ModeRegular | 0444, Size: uint64(fi.Size())},
		nil
}

func (e *OSFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	f, err := os.Open(e.path)
	if err != nil {
		return p9.QID{}, 0, err
	}
	e.f = f
	return e.qid, 4096, nil
}

func (e *OSFile) ReadAt(buf []byte, offset int64) (int, error) {
	return e.f.ReadAt(buf, offset)
}

func (e *OSFile) Close() error {
	if e.f != nil {
		return e.f.Close()
	}
	return nil
}
