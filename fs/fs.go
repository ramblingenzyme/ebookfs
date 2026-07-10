// Package fs is the composition root of the 9P frontend: it wires the registry,
// views, and inbox subpackages onto a go9p filesystem and serves it. The
// building blocks live in fs/vfile, fs/book, fs/registry, fs/views, and
// fs/inbox; this package only assembles and starts them.
package fs

import "github.com/knusbaum/go9p/fs"

func newFS() (*fs.FS, *fs.StaticDir) {
	f, root := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f, root
}
