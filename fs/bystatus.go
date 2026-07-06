package fs

type byStatusDir struct{ groupingDir }

func newByStatusDir(reg *bookRegistry) *byStatusDir {
	d := &byStatusDir{newGroupingDir(reg.f, "by-status")}
	reg.AddView(d)
	return d
}

func (d *byStatusDir) add(dir *bookDir) {
	d.listerDir(dir.Book().Meta.Status).add(dir)
}

func (d *byStatusDir) remove(dir *bookDir) {
	d.removeLister(dir.Book().Meta.Status, dir)
}
