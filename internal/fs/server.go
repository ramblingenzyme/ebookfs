package fs

import (
	"log"
	"os"

	"github.com/knusbaum/go9p"
	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/index"
	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/store"
)

func StartServer(cfg *config.Config) {
	if err := os.MkdirAll(cfg.Library.InboxTemp, 0700); err != nil {
		log.Fatalf("creating inbox temp dir: %v", err)
	}

	idx, err := index.Open(cfg.Index.Path)
	if err != nil {
		log.Fatalf("opening index: %v", err)
	}

	lib := library.New(store.New(cfg.Library.Root, cfg.Library.InboxTemp), idx)

	ebookfs, root := newFS(lib, cfg.Library.InboxTemp)
	root.AddChild(newInboxDir(ebookfs))

	log.Printf("serving 9P on %s", cfg.Server.Listen)
	go9p.Serve(cfg.Server.Listen, ebookfs.Server())
}
