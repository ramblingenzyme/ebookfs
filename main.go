package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/backend/index"
	"github.com/ramblingenzyme/ebookfs/internal/backend/kepub"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/backend/store"
	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/frontend/fs"
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
	if err := cleanInboxTemp(cfg.Library.InboxTemp); err != nil {
		log.Fatalf("cleaning inbox temp: %v", err)
	}

	if err := checkSameFilesystem(cfg.Library.Root, cfg.Library.InboxTemp); err != nil {
		log.Fatalf("inbox_temp must be on the same filesystem as library.root: %v", err)
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

// cleanInboxTemp removes stale temp files left by previous crashed writes.
// It only deletes regular files with a .epub suffix, matching the pattern
// os.CreateTemp uses in the inbox file handler.
func cleanInboxTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".epub") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			log.Printf("warning: removing stale inbox temp %q: %v", path, err)
		} else {
			log.Printf("removed stale inbox temp %q", path)
		}
	}
	return nil
}

// checkSameFilesystem verifies a and b are on the same filesystem by
// creating a temp file in one and trying os.Rename to the other. os.Rename
// across mount points returns EXDEV, so this is a portable probe of the
// actual invariant the ingest path needs.
func checkSameFilesystem(a, b string) error {
	tmp, err := os.CreateTemp(a, ".fschk-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	dst := filepath.Join(b, filepath.Base(tmp.Name()))
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(dst)
		return err
	}
	os.Remove(dst)
	return nil
}
