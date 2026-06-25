package main

import (
	"flag"
	"log"
	"os"

	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/frontend/fs"
	"github.com/ramblingenzyme/ebookfs/internal/backend/index"
	"github.com/ramblingenzyme/ebookfs/internal/backend/kepub"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/backend/store"
)

func main() {
	configPath := flag.String("config", "/etc/ebookfs/config.toml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if err := os.MkdirAll(cfg.Library.Root, 0755); err != nil {
		log.Fatalf("creating library root: %v", err)
	}
	if err := os.MkdirAll(cfg.Library.InboxTemp, 0700); err != nil {
		log.Fatalf("creating inbox temp dir: %v", err)
	}

	idx, err := index.Open(cfg.Index.Path)
	if err != nil {
		log.Fatalf("opening index: %v", err)
	}

	lib := library.New(store.New(cfg.Library.Root, cfg.Library.InboxTemp), idx)

	// The store is the source of truth; rebuild the index from it on every start
	// so a stale or missing index can't leave the served tree out of sync.
	if err := lib.Reindex(); err != nil {
		log.Fatalf("reindexing library: %v", err)
	}

	// The reader/ export serves either original epubs or converted kepubs, chosen
	// here so neither the frontend nor the library facade knows which format wins.
	// The kepub cache lives outside the library root so the store walk never sees it.
	var exporter fs.Exporter
	if cfg.Reader.Convert {
		if err := os.MkdirAll(cfg.Reader.CacheDir, 0755); err != nil {
			log.Fatalf("creating kepub cache dir: %v", err)
		}
		exporter = kepub.NewCache(cfg.Reader.CacheDir, lib)
	} else {
		exporter = fs.NewEpubExporter(lib)
	}

	readerCfg := fs.ReaderConfig{Statuses: cfg.Reader.Statuses, Convert: cfg.Reader.Convert}
	fs.StartServer(lib, exporter, readerCfg, cfg.Server.Listen, cfg.Library.InboxTemp)
}
