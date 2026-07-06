package fs

type byAuthorDir struct{ groupingDir }

func newByAuthorDir(reg *bookRegistry) *byAuthorDir {
	d := &byAuthorDir{newGroupingDir(reg.f, "by-author")}
	reg.AddView(d)
	return d
}

func (d *byAuthorDir) add(dir *bookDir) {
	for _, a := range dir.Book().Authors {
		d.listerDir(a.Name).add(dir)
	}
}

func (d *byAuthorDir) remove(dir *bookDir) {
	for _, a := range dir.Book().Authors {
		d.removeLister(a.Name, dir)
	}
}
