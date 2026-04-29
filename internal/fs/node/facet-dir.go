package node

import (
	"context"
	"fmt"
	"hash/fnv"

	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/query"
	"github.com/hugelgupf/p9/fsimpl/templatefs"
	"github.com/hugelgupf/p9/p9"
)

type FacetDir struct {
	templatefs.NoopFile
	qid         p9.QID
	lib         library.Library
	constraints query.Query
	facet       string
}

func newFacetDir(lib library.Library, constraints query.Query, facet string) *FacetDir {
	return &FacetDir{
		qid:         facetDirQID(constraints, facet),
		lib:         lib,
		constraints: constraints,
		facet:       facet,
	}
}

func facetDirQID(constraints query.Query, facet string) p9.QID {
	h := fnv.New64a()
	for _, p := range constraints {
		fmt.Fprintf(h, "%s\x00%s\x00", p.Type, p.Value)
	}
	fmt.Fprintf(h, ":\x00%s\x00", facet)
	return p9.QID{Type: p9.TypeDir, Path: h.Sum64()}
}

func (f *FacetDir) Walk(names []string) ([]p9.QID, p9.File, error) {
	if len(names) == 0 {
		return nil, f, nil
	}
	child := newQueryNode(f.lib,
		append(f.constraints[:len(f.constraints):len(f.constraints)],
			query.Predicate{Type: f.facet, Value: names[0]}))
	qs, next, err := child.Walk(names[1:])
	return append([]p9.QID{child.qid}, qs...), next, err
}

func (f *FacetDir) GetAttr(req p9.AttrMask) (p9.QID, p9.AttrMask, p9.Attr, error) {
	return f.qid,
		p9.AttrMask{Mode: true, NLink: true},
		p9.Attr{Mode: p9.ModeDirectory | 0555, NLink: 2},
		nil
}

func (f *FacetDir) Open(mode p9.OpenFlags) (p9.QID, uint32, error) {
	return f.qid, 4096, nil
}

func (f *FacetDir) Readdir(offset uint64, count uint32) (p9.Dirents, error) {
	values, err := f.lib.Values(context.Background(), f.facet, f.constraints)
	if err != nil {
		return nil, err
	}
	var all p9.Dirents
	for i, v := range values {
		newConstraints := append(f.constraints[:len(f.constraints):len(f.constraints)],
			query.Predicate{Type: f.facet, Value: v})
		all = append(all, p9.Dirent{
			QID:    queryNodeQID(newConstraints),
			Offset: uint64(i + 1),
			Type:   p9.TypeDir,
			Name:   v,
		})
	}
	if offset >= uint64(len(all)) {
		return nil, nil
	}
	return all[offset:], nil
}
