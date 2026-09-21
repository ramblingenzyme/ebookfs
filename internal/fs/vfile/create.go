package vfile

import (
	"errors"

	"github.com/knusbaum/go9p/fs"
)

// Creator is implemented by directories that accept 9P file creation. The
// FS-wide create hook (DispatchCreate) routes each create to the parent
// directory, so no single package owns the whole tree's create policy.
type Creator interface {
	Create(f *fs.FS, name string, perm uint32, mode uint8) (fs.File, error)
}

func DispatchCreate(f *fs.FS, parent fs.Dir, _, name string, perm uint32, mode uint8) (fs.File, error) {
	c, ok := parent.(Creator)
	if !ok {
		return nil, errors.New("cannot create files here")
	}
	return c.Create(f, name, perm, mode)
}
