package node

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/query"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type QueryNode struct {
	templatefs.NoopFile
	qid         p9.QID
	lib         library.Library
	constraints query.Query
}

func newQueryNode(lib library.Library, constraints query.Query) *QueryNode {
	return &QueryNode{
		qid:         queryNodeQID(constraints),
		lib:         lib,
		constraints: constraints,
	}
}

func queryNodeQID(constraints query.Query) p9.QID {
	h := fnv.New64a()
	for _, p := range constraints {
		fmt.Fprintf(h, "%s\x00%s\x00", p.Type, p.Value)
	}
	return p9.QID{Type: p9.TypeDir, Path: h.Sum64()}
}

func (q *QueryNode) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, q, nil
	}

	name := names[0]
	var next p9.File
	var nextQID p9.QID

	if strings.HasSuffix(name, ":") {
		child := newFacetDir(q.lib, q.constraints, strings.TrimSuffix(name, ":"))
		nextQID = child.qid
		next = child
	} else {
		book, err := q.bookByTitle(name)
		if err != nil {
			return nil, nil, err
		}
		ebook := newEbookDir(book)
		nextQID = ebook.qid
		next = ebook
	}

	qs, f, err := next.Walk(names[1:])
	return append([]p9.QID{nextQID}, qs...), f, err
}

func (q *QueryNode) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return q.qid,
		p9.AttrMask{Mode: true, NLink: true},
		p9.Attr{Mode: p9.ModeDirectory | 0755, NLink: 2},
		nil
}

func (q *QueryNode) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return q.qid, 4096, nil
}

func (q *QueryNode) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	ctx := context.Background()
	var all p9.Dirents

	types, err := q.lib.PredicateTypes(ctx, q.constraints)
	if err != nil {
		return nil, err
	}
	for i, t := range types {
		all = append(all, p9.Dirent{
			QID:    facetDirQID(q.constraints, t),
			Offset: uint64(i + 1),
			Type:   p9.TypeDir,
			Name:   t + ":",
		})
	}

	books, err := q.lib.Books(ctx, q.constraints)
	if err != nil {
		return nil, err
	}
	for i, b := range books {
		ebook := newEbookDir(b)
		all = append(all, p9.Dirent{
			QID:    ebook.qid,
			Offset: uint64(len(types) + i + 1),
			Type:   p9.TypeDir,
			Name:   b.Title,
		})
	}

	if offset >= uint64(len(all)) {
		return nil, nil
	}
	return all[offset:], nil
}

func (q *QueryNode) UnlinkAt(name string, flags uint32) error {
	book, err := q.bookByTitle(name)
	if err != nil {
		return err
	}
	return q.lib.DeleteBook(context.Background(), book.ID)
}

func (q *QueryNode) bookByTitle(title string) (library.Book, error) {
	books, err := q.lib.Books(context.Background(), q.constraints)
	if err != nil {
		return library.Book{}, err
	}
	for _, b := range books {
		if b.Title == title {
			return b, nil
		}
	}
	return library.Book{}, syscall.ENOENT
}
