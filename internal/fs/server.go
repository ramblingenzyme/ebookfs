package fs

import (
	"github.com/knusbaum/go9p"
)


func StartServer() {
	ebookfs, root := getFS()
	root.AddChild(newInboxDir(ebookfs))

	go9p.Serve("localhost:8002", ebookfs.Server())
}
