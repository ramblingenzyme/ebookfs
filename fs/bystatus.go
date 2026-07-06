package fs

import "github.com/knusbaum/go9p/proto"

type byStatusDir struct{ groupingDir }

func newByStatusDir(reg *bookRegistry) *byStatusDir {
	d := &byStatusDir{newGroupingDir(reg.f, "by-status")}
	reg.AddView(d)
	return d
}

func (d *byStatusDir) statusDir(name string) bookLister {
	if child, ok := d.Children()[name]; ok {
		return child.(bookLister)
	}
	sd := newBooksDir(d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR))
	d.StaticDir.AddChild(sd)
	return sd
}

func (d *byStatusDir) add(dir *bookDir) {
	d.statusDir(dir.Book().Meta.Status).add(dir)
}

func (d *byStatusDir) remove(dir *bookDir) {
	name := dir.Book().Meta.Status
	if child, ok := d.Children()[name]; ok {
		child.(bookLister).remove(dir)
		d.pruneEmpty(name)
	}
}
