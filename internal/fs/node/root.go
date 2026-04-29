package node

import (
	"syscall"

	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

const (
	qidRoot      uint64 = 1
	qidReadme    uint64 = 2
	qidImport    uint64 = 3
	qidImportLog uint64 = 4
)

type Root struct {
	templatefs.NoopFile
	lib       library.Library
	importDir *ImportDir
}

func NewRoot(lib library.Library) *Root {
	log := &StreamLog{}
	return &Root{
		lib: lib,
		importDir: newImportDir(lib, log,
			p9.QID{Type: p9.TypeDir, Path: qidImport},
			p9.QID{Type: p9.TypeRegular, Path: qidImportLog},
		),
	}
}

func (r *Root) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, r, nil
	}
	switch names[0] {
	case "README":
		if len(names) > 1 {
			return nil, nil, syscall.ENOTDIR
		}
		return []p9.QID{{Type: p9.TypeRegular, Path: qidReadme}}, &readmeFile{}, nil
	case "books":
		child := newQueryNode(r.lib, nil)
		qs, f, err := child.Walk(names[1:])
		return append([]p9.QID{child.qid}, qs...), f, err
	case "import":
		qs, f, err := r.importDir.Walk(names[1:])
		return append([]p9.QID{r.importDir.qid}, qs...), f, err
	}
	return nil, nil, syscall.ENOENT
}

func (r *Root) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return p9.QID{Type: p9.TypeDir, Path: qidRoot},
		p9.AttrMask{Mode: true, NLink: true},
		p9.Attr{Mode: p9.ModeDirectory | 0555, NLink: 2},
		nil
}

func (r *Root) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return p9.QID{Type: p9.TypeDir, Path: qidRoot}, 4096, nil
}

func (r *Root) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	all := p9.Dirents{
		{QID: p9.QID{Type: p9.TypeRegular, Path: qidReadme}, Offset: 1, Type: p9.TypeRegular, Name: "README"},
		{QID: newQueryNode(r.lib, nil).qid, Offset: 2, Type: p9.TypeDir, Name: "books"},
		{QID: r.importDir.qid, Offset: 3, Type: p9.TypeDir, Name: "import"},
	}
	if offset >= uint64(len(all)) {
		return nil, nil
	}
	return all[offset:], nil
}

type readmeFile struct{ templatefs.NoopFile }

var readmeContent = []byte("ebook-9p: Plan 9 filesystem server\n")

func (f *readmeFile) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, &readmeFile{}, nil
	}
	return nil, nil, syscall.ENOTDIR
}

func (f *readmeFile) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return p9.QID{Type: p9.TypeRegular, Path: qidReadme},
		p9.AttrMask{Mode: true, Size: true},
		p9.Attr{Mode: p9.ModeRegular | 0444, Size: uint64(len(readmeContent))},
		nil
}

func (f *readmeFile) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return p9.QID{Type: p9.TypeRegular, Path: qidReadme}, 4096, nil
}

func (f *readmeFile) ReadAt(buf []byte, offset int64) (int, error) {
	if offset >= int64(len(readmeContent)) {
		return 0, nil
	}
	return copy(buf, readmeContent[offset:]), nil
}
