package fs

import "strings"

type byTagDir struct{ groupingDir }

func newByTagDir(reg *bookRegistry) *byTagDir {
	d := &byTagDir{newGroupingDir(reg.f, "by-tag")}
	reg.AddView(d)
	return d
}

func (d *byTagDir) add(dir *bookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		d.listerDir(name).add(dir)
	}
}

func (d *byTagDir) remove(dir *bookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		d.removeLister(name, dir)
	}
}
