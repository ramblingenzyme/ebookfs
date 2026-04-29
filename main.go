package main

import (
	"flag"
	"log"
	"net"

	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/library/dummy"
	"github.com/ramblingenzyme/ebookfs/internal/fs/node"
	"github.com/hugelgupf/p9/p9"
)

type FS struct {
	lib library.Library
}

func (f *FS) Attach() (p9.File, error) {
	return node.NewRoot(f.lib), nil
}

func main() {
	addr := flag.String("addr", "localhost:9999", "listen address")
	flag.Parse()

	lib := dummy.New()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("serving 9P on %s", *addr)
	if err := p9.NewServer(&FS{lib: lib}).Serve(ln); err != nil {
		log.Fatal(err)
	}
}
