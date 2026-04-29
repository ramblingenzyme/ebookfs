package node

import (
	"syscall"

	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

// bookQIDBase offsets book-derived QIDs above the static node QIDs.
const bookQIDBase uint64 = 1000

type EbookDir struct {
	templatefs.NoopFile
	qid      p9.QID
	bookID   int64
	epub     *OSFile
	cover    *OSFile
	metadata *OSFile
}

func newEbookDir(book library.Book) *EbookDir {
	base := bookQIDBase + uint64(book.ID)*4
	return &EbookDir{
		qid:      p9.QID{Type: p9.TypeDir, Path: base},
		bookID:   book.ID,
		epub:     &OSFile{path: book.EpubPath, qid: p9.QID{Type: p9.TypeRegular, Path: base + 1}},
		cover:    &OSFile{path: book.CoverPath, qid: p9.QID{Type: p9.TypeRegular, Path: base + 2}},
		metadata: &OSFile{path: book.MetadataPath, qid: p9.QID{Type: p9.TypeRegular, Path: base + 3}},
	}
}

func (d *EbookDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, &EbookDir{qid: d.qid, bookID: d.bookID, epub: d.epub, cover: d.cover, metadata: d.metadata}, nil
	}
	var child *OSFile
	switch names[0] {
	case "book.epub":
		child = d.epub
	case "cover.jpg":
		child = d.cover
	case "metadata":
		child = d.metadata
	default:
		return nil, nil, syscall.ENOENT
	}
	if len(names) > 1 {
		return nil, nil, syscall.ENOTDIR
	}
	return []p9.QID{child.qid}, &OSFile{path: child.path, qid: child.qid}, nil
}

func (d *EbookDir) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return d.qid,
		p9.AttrMask{Mode: true, NLink: true},
		p9.Attr{Mode: p9.ModeDirectory | 0555, NLink: 2},
		nil
}

func (d *EbookDir) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return d.qid, 4096, nil
}

func (d *EbookDir) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	all := p9.Dirents{
		{QID: d.epub.qid, Offset: 1, Type: p9.TypeRegular, Name: "book.epub"},
		{QID: d.cover.qid, Offset: 2, Type: p9.TypeRegular, Name: "cover.jpg"},
		{QID: d.metadata.qid, Offset: 3, Type: p9.TypeRegular, Name: "metadata"},
	}
	if offset >= uint64(len(all)) {
		return nil, nil
	}
	return all[offset:], nil
}
