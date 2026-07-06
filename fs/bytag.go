package fs

import (
	"strings"

	"github.com/knusbaum/go9p/proto"
)

type byTagDir struct{ groupingDir }

func newByTagDir(reg *bookRegistry) *byTagDir {
	d := &byTagDir{newGroupingDir(reg.f, "by-tag")}
	reg.AddView(d)
	return d
}

func (d *byTagDir) tagDir(name string) bookLister {
	if child, ok := d.Children()[name]; ok {
		return child.(bookLister)
	}
	td := newBooksDir(d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR))
	d.StaticDir.AddChild(td)
	return td
}

func (d *byTagDir) add(dir *bookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		d.tagDir(name).add(dir)
	}
}

func (d *byTagDir) remove(dir *bookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		if child, ok := d.Children()[name]; ok {
			child.(bookLister).remove(dir)
			d.pruneEmpty(name)
		}
	}
}
